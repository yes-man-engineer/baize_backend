package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yes-man-engineer/baize_backend/internal/llm"
	"github.com/yes-man-engineer/baize_backend/internal/model"
	"github.com/yes-man-engineer/baize_backend/internal/repository"
)

// ErrMustChoosePath 入口 B 还没选路就想要方案。
var ErrMustChoosePath = errors.New("请先从候选路径里选一条")

// Progress 进度只有一个来源：绿色占比。
type Progress struct {
	Total      int `json:"total"`
	Green      int `json:"green"`
	Yellow     int `json:"yellow"`
	Red        int `json:"red"`
	GreenRatio int `json:"green_ratio"` // 0-100
	TaskTotal  int `json:"task_total"`  // 任务板上出现过的条目数（黄 + 红 + 已回填）
	TaskDone   int `json:"task_done"`   // 已回填的条目数
}

// Detail 前端一次拿全：方案文档、任务板、候选路径、对话、进度。
type Detail struct {
	Project      *model.Project     `json:"project"`
	Items        []model.PlanItem   `json:"items"`
	Tasks        []model.PlanItem   `json:"tasks"`
	Paths        []model.PathOption `json:"paths"`
	SelectedPath *model.PathOption  `json:"selected_path,omitempty"`
	Messages     []model.Message    `json:"messages"`
	Progress     Progress           `json:"progress"`
	NextAction   NextAction         `json:"next_action"`
}

type PlanService struct {
	projects *repository.ProjectRepo
	messages *repository.MessageRepo
	items    *repository.PlanItemRepo
	paths    *repository.PathOptionRepo
	ai       *llm.Client
}

func NewPlanService(
	p *repository.ProjectRepo,
	m *repository.MessageRepo,
	i *repository.PlanItemRepo,
	pa *repository.PathOptionRepo,
	ai *llm.Client,
) *PlanService {
	return &PlanService{projects: p, messages: m, items: i, paths: pa, ai: ai}
}

// Generate 生成（或重新生成）方案。用户点「够了，先给我方案」也走这里。
func (s *PlanService) Generate(ctx context.Context, token string) (*Detail, error) {
	p, err := s.projects.GetByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if p.Status == model.StatusEnded {
		return nil, ErrProjectEnded
	}
	// 入口 B 必须先选定一条路，否则方案不知道该围绕什么写。
	if p.Status == model.StatusScouting || p.Status == model.StatusChoosing {
		return nil, ErrMustChoosePath
	}

	history, err := s.messages.ListByProject(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if len(history) == 0 {
		return nil, ErrNoConversation
	}

	// 已核实的条目是用户自己跑出来的真实数据。重新生成方案时不能把它们冲掉——
	// 绿色占比是这个产品唯一的进度感，冲掉一次，用户一周的作业就白做了。
	old, err := s.items.ListByProject(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	kept := make([]model.PlanItem, 0, len(old))
	seen := make(map[string]bool, len(old))
	for _, it := range old {
		if it.VerifiedAt != nil {
			kept = append(kept, it)
			seen[itemKey(it.Section, it.Title)] = true
		}
	}

	var result llm.PlanResult
	if err := s.ai.ChatJSON(ctx, llm.PlanMessages(transcript(p, history)), &result); err != nil {
		return nil, err
	}

	verdict := model.Verdict(strings.TrimSpace(result.Verdict))
	if verdict != model.VerdictStop {
		verdict = model.VerdictGo
	}

	// 已核实的排在前面：用户一打开就看见自己跑出来的东西还在。
	items := kept
	for _, it := range result.Items {
		section := strings.TrimSpace(it.Section)
		title := strings.TrimSpace(it.Title)

		// 规则 3：劝退时不落创业执行方案。模型嘴上说 stop、手上还是给一份
		// 完整方案的情况很常见，用户看见方案就把劝退忽略了。
		if verdict == model.VerdictStop && !stopSections[section] {
			continue
		}
		if seen[itemKey(section, title)] {
			continue
		}
		seen[itemKey(section, title)] = true

		item := model.PlanItem{
			ProjectID:    p.ID,
			Section:      section,
			Title:        title,
			Content:      strings.TrimSpace(it.Content),
			Confidence:   model.Confidence(strings.TrimSpace(it.Confidence)),
			Assumption:   strings.TrimSpace(it.Assumption),
			VerifyAction: strings.TrimSpace(it.VerifyAction),
		}
		// 只规范模型新生成的条目。已核实的条目是用户填的事实，
		// 要是也走一遍降级，"摊位费 80 一天"会被关键词打回 red。
		normalizeItem(&item, p.City)
		items = append(items, item)
	}

	for i := range items {
		items[i].SortOrder = i
	}

	if err := s.items.ReplaceAll(ctx, p.ID, items); err != nil {
		return nil, err
	}

	p.Verdict = verdict
	p.VerdictReason = strings.TrimSpace(result.VerdictReason)
	if verdict == model.VerdictStop && p.VerdictReason == "" {
		p.VerdictReason = "以你现在的情况，先别急着投钱。先找份稳定的收入，攒够本钱再回来。"
	}
	p.Status = model.StatusPlanned
	if err := s.projects.Save(ctx, p); err != nil {
		return nil, err
	}

	return s.Detail(ctx, token)
}

// Verify 用户回填真实数据，这一条立刻变绿。
func (s *PlanService) Verify(ctx context.Context, token, itemID, answer string) (*model.PlanItem, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return nil, ErrEmptyAnswer
	}

	p, err := s.projects.GetByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if p.Status == model.StatusEnded {
		return nil, ErrProjectEnded
	}

	item, err := s.items.GetInProject(ctx, p.ID, itemID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	item.Answer = answer
	item.Confidence = model.ConfGreen
	item.VerifiedAt = &now

	if err := s.items.Save(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

// End 用户主动点「项目结束」。放弃是一个要主动做的动作，不是静默流失。
func (s *PlanService) End(ctx context.Context, token string) (*model.Project, error) {
	p, err := s.projects.GetByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if p.Status == model.StatusEnded {
		return p, nil
	}

	now := time.Now()
	p.Status = model.StatusEnded
	p.EndedAt = &now
	if err := s.projects.Save(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *PlanService) Detail(ctx context.Context, token string) (*Detail, error) {
	p, err := s.projects.GetByToken(ctx, token)
	if err != nil {
		return nil, err
	}

	items, err := s.items.ListByProject(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	history, err := s.messages.ListByProject(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	paths, err := s.paths.ListByProject(ctx, p.ID)
	if err != nil {
		return nil, err
	}

	tasks := make([]model.PlanItem, 0, len(items))
	var pr Progress
	pr.Total = len(items)
	for _, it := range items {
		switch it.Confidence {
		case model.ConfGreen:
			pr.Green++
		case model.ConfYellow:
			pr.Yellow++
		case model.ConfRed:
			pr.Red++
		}
		if it.VerifiedAt != nil {
			pr.TaskDone++
		}
		if it.IsTask() {
			tasks = append(tasks, it)
		}
	}
	pr.TaskTotal = len(tasks) + pr.TaskDone
	if pr.Total > 0 {
		pr.GreenRatio = pr.Green * 100 / pr.Total
	}

	var selected *model.PathOption
	if p.SelectedPathID != "" {
		for i := range paths {
			if paths[i].ID == p.SelectedPathID {
				selected = &paths[i]
				break
			}
		}
	}

	return &Detail{
		Project:      p,
		Items:        items,
		Tasks:        tasks,
		Paths:        paths,
		SelectedPath: selected,
		Messages:     history,
		Progress:     pr,
		NextAction:   nextActionFor(p),
	}, nil
}

// nextActionFor 前端刷新页面时靠这个知道该往哪走。
func nextActionFor(p *model.Project) NextAction {
	switch p.Status {
	case model.StatusScouting, model.StatusInterviewing:
		if p.AskedCount == 0 {
			return ActionOpening
		}
		return ActionAsk
	case model.StatusChoosing:
		return ActionPaths
	default:
		return ActionPlan
	}
}

// transcript 把对话拼成给模型看的纯文本，并补上已经确认的结构化事实。
func transcript(p *model.Project, history []model.Message) string {
	var b strings.Builder

	b.WriteString("【已确认的信息】\n")
	if p.Idea != "" {
		b.WriteString("想做的事: " + p.Idea + "\n")
	}
	if p.City != "" {
		b.WriteString("所在地点: " + p.City + "\n")
	}
	if p.RiskBudget >= 0 {
		b.WriteString(fmt.Sprintf("预算: %d 元（方案的投入不得超过这个数）\n", p.RiskBudget))
	} else {
		b.WriteString("预算: 未知（按最保守的口径给方案）\n")
	}
	if p.WeeklyHours > 0 {
		b.WriteString(fmt.Sprintf("每周投入: %d 小时\n", p.WeeklyHours))
	}
	if p.Assets != "" {
		b.WriteString("手上现成有: " + p.Assets + "\n")
	}
	if p.Experience != "" {
		b.WriteString("经历: " + p.Experience + "\n")
	}

	b.WriteString("\n【对话记录】\n")
	b.WriteString(dialogue(history))
	return b.String()
}

// dialogue 把消息列表拼成「顾问/用户」的纯文本对话。
func dialogue(history []model.Message) string {
	var b strings.Builder
	for _, m := range history {
		if m.Role == model.RoleAssistant {
			b.WriteString("顾问: ")
		} else {
			b.WriteString("用户: ")
		}
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// stopSections 劝退时还留在方案里的 section。
// 选址、选品、定价、采购设备、第一周计划这些是执行方案，
// 一边说"先别做"一边把它们落库，用户看一眼就当成"其实还是能做"，
// 劝退就白劝了。能说"不要做"是这个产品和满大街创业内容唯一的分界线。
var stopSections = map[string]bool{
	"风险":   true,
	"启动资金": true,
	"成本毛利": true,
}

// 规则 4 的词表。原来是一张大表做全文匹配，命中任何一个词就把 green 打成 red，
// 结果误伤率接近 100%：真正有价值的干货必然会顺口提到城管、客流、同行。
// 实测一条纯通用的产能计算（"60cm烤炉单层摆20-25串，单人稳态每晚80-120串"）
// 只因为正文里有"考虑客流间隙"四个字就被打成 red，用户看到的就是满屏待办没有干货。
// 现在拆成两类：本身就是本地事实的单独出现就降，只是话题词的必须带地点指代才降。

// localFacts 本身就是"某个具体地方多少钱"，单独出现就够。
var localFacts = []string{"摊位费", "租金", "房租"}

// localTopics 只是话题，本身不构成本地断言。
// "考虑客流间隙"是通用产能计算，"这条街客流三百人"才是装懂。
var localTopics = []string{
	"人流", "客流", "人气", "城管", "竞争", "同行", "对手",
	"日均", "几个摊", "几家店", "生意最好",
}

// placeMarkers 地点指代。话题词配上它才算指向了某个具体地方。
var placeMarkers = []string{"这条街", "那条街", "这一片", "附近", "周边", "隔壁", "对面"}

// placeNameRe 从用户填的地址里拆出地名（"成都双流区东升街道志翔路"
// 拆成 双流区 / 东升街道 / 志翔路），这样模型直接拿路名做断言也抓得住。
var placeNameRe = regexp.MustCompile(`\p{Han}{2,4}?(?:路|街道|街|巷|镇|区|县|市|村|小区|夜市|市场)`)

// normalizeItem 把模型给的置信度收进产品规则里。模型不守，代码来守。
func normalizeItem(it *model.PlanItem, city string) {
	if !it.Confidence.Valid() {
		// 模型给了不认识的值，按最保守的处理：当成待验证。
		it.Confidence = model.ConfYellow
	}

	// 规则 4：涉及具体街道的人流、价格、竞争，一律不许标 green。
	// 降成 red 而不是 yellow —— 这类事本来就只有用户站在那儿才知道。
	if it.Confidence == model.ConfGreen && mentionsLocal(it.Title+it.Content, city) {
		it.Confidence = model.ConfRed
	}

	// 黄色的价值全在那个具体假设上：给用户一个错的结论去反驳，
	// 比让他从空白开始容易十倍。给不出假设的黄色就是一张空清单，
	// 那还不如老实标 red。
	if it.Confidence == model.ConfYellow && it.Assumption == "" {
		it.Confidence = model.ConfRed
	}

	switch it.Confidence {
	case model.ConfGreen:
		// 绿色条目不该带假设和待办，模型偶尔会顺手填上。
		it.Assumption = ""
		it.VerifyAction = ""
	case model.ConfRed:
		// red 是"只有用户知道的"，给假设就是装懂。
		it.Assumption = ""
	}

	// 黄红条目没给出验证动作的，兜一句话，避免任务板上出现空白任务。
	if it.Confidence != model.ConfGreen && it.VerifyAction == "" {
		it.VerifyAction = "这一条还没定，去现场问一下或看一眼，把真实情况填回来。"
	}
}

func mentionsLocal(s, city string) bool {
	for _, w := range localFacts {
		if strings.Contains(s, w) {
			return true
		}
	}

	hasTopic := false
	for _, w := range localTopics {
		if strings.Contains(s, w) {
			hasTopic = true
			break
		}
	}
	if !hasTopic {
		return false
	}

	for _, w := range placeMarkers {
		if strings.Contains(s, w) {
			return true
		}
	}
	for _, name := range placeNameRe.FindAllString(city, -1) {
		if strings.Contains(s, name) {
			return true
		}
	}
	return false
}

func itemKey(section, title string) string { return section + "\x00" + title }
