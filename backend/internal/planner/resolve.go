package planner

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/contract"
)

// 目标解析的错误码。
const (
	CodeTargetNotFound  = "target_not_found"
	CodeTargetAmbiguous = "target_ambiguous"
	CodeTargetHintEmpty = "target_hint_empty"
)

// Result 是回给模型的工具结果。模型据此决定下一步或改口。
type Result struct {
	OK         bool            `json:"ok"`
	Summary    string          `json:"summary,omitempty"`
	Warning    string          `json:"warning,omitempty"`
	Error      string          `json:"error,omitempty"`
	Detail     string          `json:"detail,omitempty"`
	Page       *PageView       `json:"page,omitempty"`
	Candidates []CandidateView `json:"candidates,omitempty"`
	Steps      []StepView      `json:"steps,omitempty"`
	Failure    *DryRunFailure  `json:"dry_run_failure,omitempty"`
}

// PageView 是给模型看的紧凑观测视图。
type PageView struct {
	URL       string        `json:"url"`
	Title     string        `json:"title"`
	Truncated bool          `json:"truncated,omitempty"`
	Elements  []ElementView `json:"elements"`
}

// ElementView 是页面上的一个可选目标。
type ElementView struct {
	Ref         string `json:"ref"`
	Tag         string `json:"tag"`
	Role        string `json:"role,omitempty"`
	Name        string `json:"name,omitempty"`
	Text        string `json:"text,omitempty"`
	Value       string `json:"value,omitempty"`
	LocatorKind string `json:"locator_kind,omitempty"`
}

// CandidateView 是解析失败时给出的候选，带匹配分。
type CandidateView struct {
	Ref   string `json:"ref"`
	Role  string `json:"role,omitempty"`
	Name  string `json:"name,omitempty"`
	Text  string `json:"text,omitempty"`
	Score int    `json:"score"`
}

// StepView 是已记录步骤的摘要，供模型复核。
type StepView struct {
	Index          int      `json:"index"`
	Action         string   `json:"action"`
	Intent         string   `json:"intent"`
	Value          string   `json:"value,omitempty"`
	Target         string   `json:"target,omitempty"`
	Preconditions  []string `json:"preconditions"`
	Postconditions []string `json:"postconditions"`
}

// DryRunStep 是干跑失败的单个步骤。
type DryRunStep struct {
	Index       int      `json:"index"`
	Action      string   `json:"action"`
	Status      string   `json:"status"`
	URL         string   `json:"url,omitempty"`
	Error       string   `json:"error,omitempty"`
	Unsatisfied []string `json:"unsatisfied_conditions,omitempty"`
}

// DryRunFailure 是干跑失败的汇总，直接回给模型让它改。
type DryRunFailure struct {
	ExecutionID string       `json:"execution_id"`
	Status      string       `json:"status"`
	FinalURL    string       `json:"final_url,omitempty"`
	Error       string       `json:"error,omitempty"`
	Steps       []DryRunStep `json:"steps"`
}

type targetError struct {
	code    string
	message string
}

func (e *targetError) Error() string { return e.message }

func targetFailure(code, format string, args ...any) *targetError {
	return &targetError{code: code, message: fmt.Sprintf(format, args...)}
}

type scoredElement struct {
	element contract.Element
	score   int
}

// resolve 把模型的 hint 解析成唯一元素与已验证定位器。
//
// 只在"可见且可用"的元素里匹配；命中不唯一时一律拒绝，要求模型说得更具体。
// 这一步是"AI 生成错的目标"的拦截点。
func resolve(
	hint string, elements []contract.Element,
) (contract.Element, contract.Locator, []CandidateView, error) {
	if strings.TrimSpace(hint) == "" {
		return contract.Element{}, contract.Locator{}, nil,
			targetFailure(CodeTargetHintEmpty, "target hint must not be empty")
	}
	actionable := make([]scoredElement, 0, len(elements))
	for _, element := range elements {
		if !element.Visible || !element.Enabled || len(element.Locators) == 0 {
			continue
		}
		actionable = append(actionable, scoredElement{element: element, score: matchScore(hint, element)})
	}
	sort.SliceStable(actionable, func(i, j int) bool {
		if actionable[i].score != actionable[j].score {
			return actionable[i].score > actionable[j].score
		}
		return actionable[i].element.Ref < actionable[j].element.Ref
	})

	describe := func(limit int, includeZero bool) []CandidateView {
		views := make([]CandidateView, 0, limit)
		for _, item := range actionable {
			if len(views) >= limit {
				break
			}
			if item.score == 0 && !includeZero {
				continue
			}
			views = append(views, CandidateView{
				Ref:   item.element.Ref,
				Role:  item.element.Role,
				Name:  item.element.Name,
				Text:  item.element.Text,
				Score: item.score,
			})
		}
		return views
	}

	if len(actionable) == 0 {
		return contract.Element{}, contract.Locator{}, nil, targetFailure(
			CodeTargetNotFound,
			"the current page has no visible, enabled element to target; call open_page to move to another page",
		)
	}
	best := actionable[0]
	if best.score == 0 {
		return contract.Element{}, contract.Locator{}, describe(10, true), targetFailure(
			CodeTargetNotFound,
			"no element matches hint %q; pick one of the candidates or use a hint that appears verbatim in name/text",
			hint,
		)
	}
	if len(actionable) > 1 && actionable[1].score == best.score {
		return contract.Element{}, contract.Locator{}, describe(10, false), targetFailure(
			CodeTargetAmbiguous,
			"hint %q matches %d elements equally well; use a more specific hint",
			hint, countTopScore(actionable, best.score),
		)
	}
	return best.element, best.element.Locators[0], nil, nil
}

func countTopScore(items []scoredElement, score int) int {
	count := 0
	for _, item := range items {
		if item.score == score {
			count++
		}
	}
	return count
}

// matchScore 给元素与 hint 的匹配程度打分。0 表示不匹配。
func matchScore(hint string, element contract.Element) int {
	needle := normalize(hint)
	name := normalize(element.Name)
	text := normalize(element.Text)
	switch {
	case name != "" && name == needle:
		return 100
	case text != "" && text == needle:
		return 95
	case name != "" && strings.Contains(name, needle):
		return 80
	case text != "" && strings.Contains(text, needle):
		return 70
	case name != "" && strings.Contains(needle, name):
		return 60
	case text != "" && strings.Contains(needle, text):
		return 50
	}
	return 0
}

// normalize 统一大小写与空白，避免因排版差异错过匹配。
func normalize(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func origin(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func isAbsoluteURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.IsAbs() && parsed.Host != ""
}
