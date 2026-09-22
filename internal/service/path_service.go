package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yes-man-engineer/baize_backend/internal/llm"
	"github.com/yes-man-engineer/baize_backend/internal/model"
	"github.com/yes-man-engineer/baize_backend/internal/repository"
)

// ErrPathsIncomplete 模型没凑够 3 条。宁可让用户重试，
// 也不要把一个只有 1、2 条的选路页摆到他面前——那等于替他做了决定。
var ErrPathsIncomplete = errors.New("候选路径没能凑够 3 条")

// wantPaths 固定 3 条：给 1 条等于替用户做决定，给 10 条等于没给。
const wantPaths = 3

// PathService 管入口 B 的选路环节：盘点完 → 给 3 条路 → 选一条 → 合流回主干。
type PathService struct {
	projects  *repository.ProjectRepo
	messages  *repository.MessageRepo
	paths     *repository.PathOptionRepo
	interview *InterviewService
	ai        *llm.Client
}

func NewPathService(
	p *repository.ProjectRepo,
	m *repository.MessageRepo,
	pa *repository.PathOptionRepo,
	iv *InterviewService,
	ai *llm.Client,
) *PathService {
	return &PathService{projects: p, messages: m, paths: pa, interview: iv, ai: ai}
}

// Generate 根据盘点生成 3 条候选路径。用户在盘点阶段点「够了」也走这里。
func (s *PathService) Generate(ctx context.Context, token string) (*model.Project, []model.PathOption, error) {
	p, err := s.projects.GetByToken(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	if p.Status == model.StatusEnded {
		return nil, nil, ErrProjectEnded
	}
	// 只有入口 B 的盘点阶段（或重新选路）才有这一步。
	if p.Status != model.StatusScouting && p.Status != model.StatusChoosing {
		return nil, nil, ErrNotScouting
	}

	history, err := s.messages.ListByProject(ctx, p.ID)
	if err != nil {
		return nil, nil, err
	}
	if len(history) == 0 {
		return nil, nil, ErrNoConversation
	}

	var r llm.PathResult
	if err := s.ai.ChatJSON(ctx, llm.PathsMessages(profileText(p, history)), &r); err != nil {
		return nil, nil, err
	}

	if len(r.Paths) < wantPaths {
		return nil, nil, ErrPathsIncomplete
	}
	// 多给的丢掉：3 条之外的条目只会稀释选择。
	picked := r.Paths[:wantPaths]

	list := make([]model.PathOption, 0, wantPaths)
	for i, it := range picked {
		cost := it.StartupCost
		if cost < 0 {
			cost = 0
		}
		list = append(list, model.PathOption{
			ProjectID:   p.ID,
			Title:       strings.TrimSpace(it.Title),
			Angle:       model.PathAngle(strings.TrimSpace(it.Angle)),
			Summary:     strings.TrimSpace(it.Summary),
			WhyYou:      strings.TrimSpace(it.WhyYou),
			StartupCost: cost,
			FirstStep:   strings.TrimSpace(it.FirstStep),
			SortOrder:   i,
		})
	}
	spreadAngles(list)

	if err := s.paths.ReplaceAll(ctx, p.ID, list); err != nil {
		return nil, nil, err
	}

	p.Status = model.StatusChoosing
	p.SelectedPathID = ""
	if err := s.projects.Save(ctx, p); err != nil {
		return nil, nil, err
	}

	saved, err := s.paths.ListByProject(ctx, p.ID)
	if err != nil {
		return nil, nil, err
	}
	return p, saved, nil
}

// Select 选定一条路径，然后立刻合流回主干提问。
func (s *PathService) Select(ctx context.Context, token, pathID string) (*model.Project, string, NextAction, error) {
	p, err := s.projects.GetByToken(ctx, token)
	if err != nil {
		return nil, "", "", err
	}
	if p.Status == model.StatusEnded {
		return nil, "", "", ErrProjectEnded
	}
	if p.Status != model.StatusChoosing {
		return nil, "", "", ErrNotChoosing
	}

	path, err := s.paths.GetInProject(ctx, p.ID, pathID)
	if err != nil {
		return nil, "", "", err
	}

	// 把选择写进对话，后面生成方案时模型能看到用户选了哪条、为什么。
	choice := fmt.Sprintf("我选这条：%s。%s", path.Title, path.Summary)
	if err := s.messages.Create(ctx, &model.Message{
		ProjectID: p.ID, Role: model.RoleUser, Content: choice,
	}); err != nil {
		return nil, "", "", err
	}

	p.SelectedPathID = path.ID
	p.Idea = path.Title
	p.Status = model.StatusInterviewing
	if err := s.projects.Save(ctx, p); err != nil {
		return nil, "", "", err
	}

	// 合流：选完立刻按主干规则继续问，问透了就直接出方案。
	history, err := s.messages.ListByProject(ctx, p.ID)
	if err != nil {
		return nil, "", "", err
	}

	// 这里已经选定了一条路，不存在想法空泛的问题，第三个返回值丢掉。
	question, done, _, err := s.interview.askInterview(ctx, p, history)
	if err != nil {
		return nil, "", "", err
	}

	next := ActionAsk
	if done {
		next = ActionPlan
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

// spreadAngles 保证 3 条路径的角度互不相同。
// 模型除了给不认识的值，还常常三条全标 steady —— 那跟只给一条没区别。
// 角度是把 3 条拉开的唯一手段，重复的按位置补成还没被占用的那个。
func spreadAngles(list []model.PathOption) {
	all := []model.PathAngle{model.AngleSteady, model.AngleFastCash, model.AngleHighCeiling}

	used := make(map[model.PathAngle]bool, len(all))
	need := make([]int, 0, len(list))
	for i := range list {
		a := list[i].Angle
		if !a.Valid() || used[a] {
			need = append(need, i)
			continue
		}
		used[a] = true
	}

	free := make([]model.PathAngle, 0, len(all))
	for _, a := range all {
		if !used[a] {
			free = append(free, a)
		}
	}
	for n, i := range need {
		if n >= len(free) {
			break
		}
		list[i].Angle = free[n]
	}
}

// profileText 把盘点结果拼成给模型看的纯文本。
func profileText(p *model.Project, history []model.Message) string {
	var b strings.Builder

	b.WriteString("【盘点到的事实】\n")
	if p.City != "" {
		b.WriteString("所在地点: " + p.City + "\n")
	}
	if p.RiskBudget >= 0 {
		b.WriteString(fmt.Sprintf("最多能亏: %d 元（每条路径的启动投入都不得超过这个数）\n", p.RiskBudget))
	} else {
		b.WriteString("最多能亏: 未知（按最保守的口径给，启动投入控制在几千元以内）\n")
	}
	if p.WeeklyHours > 0 {
		b.WriteString(fmt.Sprintf("每周投入: %d 小时\n", p.WeeklyHours))
	}
	if p.Assets != "" {
		b.WriteString("手上现成有: " + p.Assets + "\n")
	}
	if p.Experience != "" {
		b.WriteString("经历与别人常找他帮的忙: " + p.Experience + "\n")
	}

	b.WriteString("\n【盘点对话】\n")
	b.WriteString(dialogue(history))
	return b.String()
}
