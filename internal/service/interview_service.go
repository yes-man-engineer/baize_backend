package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/yes-man-engineer/baize_backend/internal/llm"
	"github.com/yes-man-engineer/baize_backend/internal/model"
	"github.com/yes-man-engineer/baize_backend/internal/repository"
	"github.com/yes-man-engineer/baize_backend/pkg/logger"
)

var (
	ErrProjectEnded   = errors.New("项目已结束")
	ErrNotAnswering   = errors.New("当前阶段不需要回答问题")
	ErrEmptyAnswer    = errors.New("回答不能为空")
	ErrNoConversation = errors.New("还没有任何对话，无法生成方案")
	ErrNotScouting    = errors.New("当前阶段不能生成候选路径")
	ErrNotChoosing    = errors.New("当前阶段不能选择路径")
)

// NextAction 告诉前端下一步该调哪个接口，省得它自己按 status 猜。
type NextAction string

const (
	// ActionOpening 项目刚建好，还差一句开场白
	ActionOpening NextAction = "opening"
	// ActionAsk 继续回答问题
	ActionAsk NextAction = "ask"
	// ActionPaths 去生成 3 条候选路径（入口 B 盘点完）
	ActionPaths NextAction = "paths"
	// ActionPlan 去生成方案
	ActionPlan NextAction = "plan"
)

// 开场白生成不出来时的兜底问法。开场不问钱，预算由模型在聊天里挑时机问。
const (
	fallbackOpeningHasIdea = "先说说看，这事你打算在哪儿做？"
	fallbackOpeningNoIdea  = "先随便聊聊——你现在平时都在忙些什么？"
)

// openingTimeout 开场白卡在对话页的等待上，宁可退回固定问法也不让用户干等。
// 实测这一句要 8 到 20 秒，大半吃在首字延迟上，留一倍余量。
const openingTimeout = 30 * time.Second

// firstRound 是提问的第一轮，AskedCount 从 1 开始计。
const firstRound = 1

type InterviewService struct {
	projects *repository.ProjectRepo
	messages *repository.MessageRepo
	ai       *llm.Client
}

func NewInterviewService(p *repository.ProjectRepo, m *repository.MessageRepo, ai *llm.Client) *InterviewService {
	return &InterviewService{projects: p, messages: m, ai: ai}
}

// Start 开一个新项目。idea 为空表示走入口 B（不知道能做什么），先进盘点。
func (s *InterviewService) Start(ctx context.Context, idea string) (*model.Project, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}

	idea = strings.TrimSpace(idea)
	entry, status := model.EntryHasIdea, model.StatusInterviewing
	if idea == "" {
		entry, status = model.EntryNoIdea, model.StatusScouting
	}

	p := &model.Project{
		Token:       token,
		Status:      status,
		Entry:       entry,
		Idea:        idea,
		RiskBudget:  -1,
		WeeklyHours: -1,
	}
	if err := s.projects.Create(ctx, p); err != nil {
		return nil, err
	}

	if idea != "" {
		if err := s.messages.Create(ctx, &model.Message{
			ProjectID: p.ID, Role: model.RoleUser, Content: idea,
		}); err != nil {
			return nil, err
		}
	}

	return p, nil
}

// Opening 生成开场白。跟建项目分开是因为这一步要等模型十几秒，
// 而前端需要拿到 token 立刻跳转，把等待放在对话页里展示。
// 重复调用直接返回已有的那句，不会重复生成。
func (s *InterviewService) Opening(ctx context.Context, token string) (*model.Project, string, NextAction, error) {
	p, err := s.projects.GetByToken(ctx, token)
	if err != nil {
		return nil, "", "", err
	}

	history, err := s.messages.ListByProject(ctx, p.ID)
	if err != nil {
		return nil, "", "", err
	}
	for _, m := range history {
		if m.Role == model.RoleAssistant {
			return p, m.Content, ActionAsk, nil
		}
	}

	question := s.openingQuestion(ctx, p)
	if err := s.messages.Create(ctx, &model.Message{
		ProjectID: p.ID, Role: model.RoleAssistant, Content: question,
	}); err != nil {
		return nil, "", "", err
	}

	p.AskedCount = firstRound
	if err := s.projects.Save(ctx, p); err != nil {
		return nil, "", "", err
	}

	return p, question, ActionAsk, nil
}

// openingQuestion 入口 A 让模型接住用户那句想法，入口 B 直接进盘点。
// 生成不出来就退回固定问法，不让用户卡在这里。
func (s *InterviewService) openingQuestion(ctx context.Context, p *model.Project) string {
	ctx, cancel := context.WithTimeout(ctx, openingTimeout)
	defer cancel()

	fallback := fallbackOpeningNoIdea
	msgs := llm.ScoutMessages(nil)
	if p.Idea != "" {
		fallback = fallbackOpeningHasIdea
		msgs = llm.OpeningMessages(p.Idea)
	}

	var r llm.OpeningResult
	if err := s.ai.ChatJSON(ctx, msgs, &r); err != nil {
		logger.Warn("[openingQuestion] 开场白生成失败，回退固定问法", zap.Error(err))
		return fallback
	}

	if q := strings.TrimSpace(r.Question); q != "" {
		return q
	}
	return fallback
}

// Answer 收下用户的回答，返回下一个问题和下一步动作。
func (s *InterviewService) Answer(ctx context.Context, token, content string) (*model.Project, string, NextAction, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, "", "", ErrEmptyAnswer
	}

	p, err := s.projects.GetByToken(ctx, token)
	if err != nil {
		return nil, "", "", err
	}
	if p.Status == model.StatusEnded {
		return nil, "", "", ErrProjectEnded
	}
	if !p.Answering() {
		return nil, "", "", ErrNotAnswering
	}

	// 预算兜底：模型漏抽时自己从这句话里捞一次，只认带单位的（"3000块""5千"）。
	// 不认裸数字——预算问在第几轮不固定了，无从判断"35"是预算还是年龄、小时数，
	// 而这个数会原样进 prompt 决定整份方案的大小。裸数字交给模型抽，它知道自己刚问了什么。
	if p.RiskBudget < 0 {
		if n, ok := parseMoney(content, false); ok {
			p.RiskBudget = n
		}
	}
	if p.WeeklyHours < 0 {
		if n, ok := parseHours(content); ok {
			p.WeeklyHours = n
		}
	}

	history, err := s.messages.ListByProject(ctx, p.ID)
	if err != nil {
		return nil, "", "", err
	}
	// 这一轮的回答先只挂在内存里给模型看。模型调用失败时用户会重发，
	// 提前落库的话库里会留下两条一样的。
	answer := model.Message{ProjectID: p.ID, Role: model.RoleUser, Content: content}
	history = append(history, answer)

	var question string
	var done bool
	if p.Status == model.StatusScouting {
		question, done, err = s.askScout(ctx, p, history)
	} else {
		var tooVague bool
		question, done, tooVague, err = s.askInterview(ctx, p, history)
		// 输入框里填的不是一个具体想法，转去盘点。只认第一轮的判断：
		// 后面几轮用户聊的是城市、时间、手上有什么，再翻盘会把人在两条路之间来回甩。
		if err == nil && tooVague && p.AskedCount == firstRound {
			p.Entry = model.EntryNoIdea
			p.Status = model.StatusScouting
			p.Idea = ""
			question, done, err = s.askScout(ctx, p, history)
		}
	}
	if err != nil {
		return nil, "", "", err
	}

	if err := s.messages.Create(ctx, &answer); err != nil {
		return nil, "", "", err
	}

	next := ActionAsk
	if done {
		// 入口 B 盘完先去选路，其他情况直接出方案。
		next = ActionPlan
		if p.Status == model.StatusScouting {
			next = ActionPaths
		}
	} else {
		if err := s.messages.Create(ctx, &model.Message{
			ProjectID: p.ID, Role: model.RoleAssistant, Content: question,
		}); err != nil {
			return nil, "", "", err
		}
		p.AskedCount++
	}

	if err := s.projects.Save(ctx, p); err != nil {
		return nil, "", "", err
	}

	return p, question, next, nil
}

// askInterview 主干提问：围绕一个已有想法往下挖。
func (s *InterviewService) askInterview(ctx context.Context, p *model.Project, history []model.Message) (string, bool, bool, error) {
	var r llm.InterviewResult
	if err := s.ai.ChatJSON(ctx, llm.InterviewMessages(toLLM(history)), &r); err != nil {
		return "", false, false, err
	}

	e := r.Extracted
	if p.Idea == "" && strings.TrimSpace(e.Idea) != "" {
		p.Idea = strings.TrimSpace(e.Idea)
	}
	if strings.TrimSpace(e.City) != "" {
		p.City = strings.TrimSpace(e.City)
	}
	if e.WeeklyHours > 0 {
		p.WeeklyHours = e.WeeklyHours
	}
	if e.Budget > 0 {
		p.RiskBudget = e.Budget
	}

	q := strings.TrimSpace(r.Question)
	// 打出来是为了能查：模型到底有没有按规则把问题挂到方案的某个格子上，
	// 还是在问换任何行当都成立的通用问题。挂不上就说明提问又退化成填表了。
	logger.Info("[askInterview] 提问",
		zap.String("hooked_item", r.HookedItem), zap.String("question", q))

	return q, r.Done || q == "", r.IdeaTooVague, nil
}

// askScout 入口 B 的盘点提问：摸清他手上现成有什么。
func (s *InterviewService) askScout(ctx context.Context, p *model.Project, history []model.Message) (string, bool, error) {
	var r llm.ScoutResult
	if err := s.ai.ChatJSON(ctx, llm.ScoutMessages(toLLM(history)), &r); err != nil {
		return "", false, err
	}

	e := r.Extracted
	if strings.TrimSpace(e.City) != "" {
		p.City = strings.TrimSpace(e.City)
	}
	if e.WeeklyHours > 0 {
		p.WeeklyHours = e.WeeklyHours
	}
	if e.Budget > 0 {
		p.RiskBudget = e.Budget
	}
	p.Assets = appendFact(p.Assets, e.Assets)
	p.Experience = appendFact(p.Experience, e.Experience)

	q := strings.TrimSpace(r.Question)
	return q, r.Done || q == "", nil
}

// appendFact 盘点是一轮轮累加的，不是覆盖。
func appendFact(old, add string) string {
	add = strings.TrimSpace(add)
	if add == "" || strings.Contains(old, add) {
		return old
	}
	if old == "" {
		return add
	}
	return old + "；" + add
}

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func toLLM(list []model.Message) []llm.Message {
	out := make([]llm.Message, 0, len(list))
	for _, m := range list {
		role := llm.RoleUser
		if m.Role == model.RoleAssistant {
			role = llm.RoleAssistant
		}
		out = append(out, llm.Message{Role: role, Content: m.Content})
	}
	return out
}

// 金额单位。带单位的数字才是可信的金额线索，"3年""35岁"抓不进来。
var moneyWithUnitRe = regexp.MustCompile(
	`([0-9]+(?:\.[0-9]+)?|[零一二两三四五六七八九十]+)\s*(万元|千元|万|千|w|W|k|K|块钱|块|元)`)

// 时间单位。和金额一样只认带单位的，"35岁""3年"抓不进来。
var hoursWithUnitRe = regexp.MustCompile(
	`([0-9]+(?:\.[0-9]+)?|[零一二两三四五六七八九十]+)\s*(?:个)?\s*(?:小时|钟头|h|H)`)

// 不带单位的裸数字，只在明确问钱的那一轮才敢采信。
var bareNumberRe = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

var cnDigits = map[rune]int{
	'零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4,
	'五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
}

// maxWeeklyHours 一周的小时数。超过这个数说明抓错了，比如把价钱当成了工时。
const maxWeeklyHours = 168

// parseHours 从"二十个小时""20小时"里抠出每周能投入的小时数。
// 模型漏抽时兜一次。只认带单位的，裸数字在这几轮里更可能是年龄、价钱。
// 一句话里有多个数就取最小的，理由和金额一样：估少了方案偏保守，估多了做不完。
func parseHours(s string) (int, bool) {
	ms := hoursWithUnitRe.FindAllStringSubmatch(s, -1)
	best, found := 0.0, false
	for _, m := range ms {
		v, ok := toFloat(m[1])
		if !ok || v <= 0 || v > maxWeeklyHours {
			continue
		}
		if !found || v < best {
			best, found = v, true
		}
	}
	if !found {
		return 0, false
	}
	return int(best), true
}

// parseMoney 从"5000块""1万左右""三五千""2w"里抠出一个数字（元）。
// allowBare 为 true 时，"5000" 这种不带单位的也认。
// 抠不出来返回 false，方案会按未知的保守口径处理——宁可不知道，
// 也不要拿一个错的数字去决定整份方案的大小。
func parseMoney(s string, allowBare bool) (int, bool) {
	if v, ok := smallestWithUnit(s); ok {
		return v, true
	}
	if !allowBare {
		return 0, false
	}
	if m := bareNumberRe.FindString(s); m != "" {
		if v, err := strconv.ParseFloat(m, 64); err == nil && v >= 0 {
			return int(v), true
		}
	}
	return 0, false
}

// smallestWithUnit 取所有带单位金额里最小的那个。
// "我有2万存款，先拿5千出来试"这种一句话里出现多个数，往小了取更安全：
// 方案做小了用户顶多觉得保守，做大了他掏不出这笔钱。
func smallestWithUnit(s string) (int, bool) {
	ms := moneyWithUnitRe.FindAllStringSubmatch(s, -1)
	if len(ms) == 0 {
		return 0, false
	}

	best, found := 0.0, false
	for _, m := range ms {
		v, ok := toFloat(m[1])
		if !ok {
			continue
		}
		switch m[2] {
		case "万", "万元", "w", "W":
			v *= 10000
		case "千", "千元", "k", "K":
			v *= 1000
		}
		if v < 0 {
			continue
		}
		if !found || v < best {
			best, found = v, true
		}
	}
	if !found {
		return 0, false
	}
	return int(best), true
}

func toFloat(s string) (float64, bool) {
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v, true
	}
	return cnToFloat([]rune(s))
}

// cnToFloat 认中文数字。够用就行：一/两/三…十/十五/二十/二十五。
func cnToFloat(rs []rune) (float64, bool) {
	if len(rs) == 0 {
		return 0, false
	}

	// 「十」「十五」
	if rs[0] == '十' {
		if len(rs) == 1 {
			return 10, true
		}
		if d, ok := cnDigits[rs[1]]; ok {
			return float64(10 + d), true
		}
		return 10, true
	}

	first, ok := cnDigits[rs[0]]
	if !ok {
		return 0, false
	}
	if len(rs) == 1 {
		return float64(first), true
	}

	// 「二十」「二十五」
	if rs[1] == '十' {
		v := first * 10
		if len(rs) > 2 {
			if d, ok := cnDigits[rs[2]]; ok {
				v += d
			}
		}
		return float64(v), true
	}

	// 「三五千」是个范围，取小的那头。
	return float64(first), true
}
