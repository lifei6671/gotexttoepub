package goepub

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	preflightReportVersion = "1"
	maxPreflightIssues     = 200
	maxEvidenceRunes       = 240

	PreflightIssueMissingChapter    = "missing_chapter"
	PreflightIssueDuplicateChapter  = "duplicate_chapter"
	PreflightIssueChapterOutOfOrder = "chapter_out_of_order"
	PreflightIssueThirdPartyAd      = "third_party_ad"

	PreflightSeverityError   = "error"
	PreflightSeverityWarning = "warning"
)

// PreflightReport 是转换前的统一异常检测报告。
type PreflightReport struct {
	Version      string           `json:"version"`
	ChapterCount int              `json:"chapterCount"`
	Issues       []PreflightIssue `json:"issues"`
	Truncated    bool             `json:"truncated"`
}

// PreflightIssue 描述一项可定位的文稿异常。
type PreflightIssue struct {
	Type            string `json:"type"`
	Severity        string `json:"severity"`
	LineNumber      int    `json:"lineNumber"`
	ChapterTitle    string `json:"chapterTitle"`
	Message         string `json:"message"`
	Evidence        string `json:"evidence"`
	Detector        string `json:"detector"`
	PreviousChapter *int   `json:"previousChapter,omitempty"`
	CurrentChapter  *int   `json:"currentChapter,omitempty"`
	MissingFrom     *int   `json:"missingFrom,omitempty"`
	MissingTo       *int   `json:"missingTo,omitempty"`
}

// PreflightError 表示预检发现异常，调用方需要让用户确认是否继续。
type PreflightError struct {
	Report PreflightReport
}

func (e *PreflightError) Error() string {
	return fmt.Sprintf("转换前检测到 %d 项文稿异常，需要确认后再继续", len(e.Report.Issues))
}

func newPreflightReport() *PreflightReport {
	return &PreflightReport{
		Version: preflightReportVersion,
		Issues:  make([]PreflightIssue, 0),
	}
}

func (report *PreflightReport) addIssue(issue PreflightIssue) {
	if len(report.Issues) >= maxPreflightIssues {
		report.Truncated = true
		return
	}
	issue.Evidence = truncateEvidence(issue.Evidence)
	report.Issues = append(report.Issues, issue)
}

func truncateEvidence(value string) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= maxEvidenceRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxEvidenceRunes]) + "…"
}

type chapterSequenceTracker struct {
	previous       *numberedChapter
	volumeBoundary bool
}

type numberedChapter struct {
	number int
	title  string
}

func (tracker *chapterSequenceTracker) startVolume() {
	tracker.volumeBoundary = true
}

func (tracker *chapterSequenceTracker) observe(report *PreflightReport, lineNumber int, title string) {
	report.ChapterCount++
	number, ok := parseChapterNumber(title)
	if !ok {
		return
	}

	current := numberedChapter{number: number, title: title}
	if tracker.previous == nil {
		tracker.previous = &current
		tracker.volumeBoundary = false
		return
	}
	if tracker.volumeBoundary && (number == 0 || number == 1) {
		tracker.previous = &current
		tracker.volumeBoundary = false
		return
	}
	tracker.volumeBoundary = false

	previous := tracker.previous
	issue := PreflightIssue{
		LineNumber:      lineNumber,
		ChapterTitle:    title,
		Evidence:        previous.title + " → " + title,
		Detector:        "chapter_sequence",
		PreviousChapter: intPointer(previous.number),
		CurrentChapter:  intPointer(number),
	}
	switch {
	case number == previous.number:
		issue.Type = PreflightIssueDuplicateChapter
		issue.Severity = PreflightSeverityError
		issue.Message = fmt.Sprintf("章节编号重复：第%d章连续出现两次", number)
		report.addIssue(issue)
	case number < previous.number:
		issue.Type = PreflightIssueChapterOutOfOrder
		issue.Severity = PreflightSeverityError
		issue.Message = fmt.Sprintf("章节疑似乱序：第%d章之后出现第%d章", previous.number, number)
		report.addIssue(issue)
	case number > previous.number+1:
		missingFrom := previous.number + 1
		missingTo := number - 1
		issue.Type = PreflightIssueMissingChapter
		issue.Severity = PreflightSeverityError
		issue.MissingFrom = intPointer(missingFrom)
		issue.MissingTo = intPointer(missingTo)
		if missingFrom == missingTo {
			issue.Message = fmt.Sprintf("疑似缺少第%d章", missingFrom)
		} else {
			issue.Message = fmt.Sprintf("疑似缺少第%d章至第%d章", missingFrom, missingTo)
		}
		report.addIssue(issue)
	}
	tracker.previous = &current
}

func intPointer(value int) *int {
	return &value
}

var chapterNumberPattern = regexp.MustCompile(`^第([一二三四五六七八九十百千万零〇两0-9]+)[章回节]`)

func parseChapterNumber(title string) (int, bool) {
	match := chapterNumberPattern.FindStringSubmatch(strings.TrimSpace(title))
	if len(match) != 2 {
		return 0, false
	}
	if number, err := strconv.Atoi(match[1]); err == nil {
		return number, true
	}
	return parseChineseNumber(match[1])
}

func parseChineseNumber(value string) (int, bool) {
	digits := map[rune]int{
		'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3,
		'四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
	}
	units := map[rune]int{'十': 10, '百': 100, '千': 1000, '万': 10000}

	hasUnit := false
	for _, char := range value {
		if _, ok := units[char]; ok {
			hasUnit = true
			break
		}
	}
	if !hasUnit {
		number := 0
		for _, char := range value {
			digit, ok := digits[char]
			if !ok {
				return 0, false
			}
			number = number*10 + digit
		}
		return number, true
	}

	total, section, current := 0, 0, 0
	for _, char := range value {
		if digit, ok := digits[char]; ok {
			current = digit
			continue
		}
		unit, ok := units[char]
		if !ok {
			return 0, false
		}
		if unit == 10000 {
			section += current
			if section == 0 {
				section = 1
			}
			total += section * unit
			section, current = 0, 0
			continue
		}
		if current == 0 {
			current = 1
		}
		section += current * unit
		current = 0
	}
	return total + section + current, true
}

// thirdPartyDetector 隔离第三方引流规则，后续新增来源只需追加检测器实现。
type thirdPartyDetector interface {
	Name() string
	Detect(line string) (message string, matched bool)
}

type promotionCombinationDetector struct{}

func (promotionCombinationDetector) Name() string {
	return "third_party_promotion_combination"
}

var webAddressPattern = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s，。；、]+|(?:[a-z0-9][a-z0-9-]*\.)+(?:com|cn|net|org|cc|vip|top)(?:/[^\s，。；、]*)?`)

func (promotionCombinationDetector) Detect(line string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(line))
	if normalized == "" {
		return "", false
	}

	if webAddressPattern.MatchString(normalized) && containsAny(normalized,
		"请访问", "请记住", "最新网址", "最新地址", "备用网址", "更多精彩",
		"最新章节", "全文阅读", "小说网", "下载", "搜索本站",
	) {
		return "疑似包含第三方网站引流信息", true
	}
	if webAddressPattern.MatchString(normalized) {
		return "疑似包含第三方网站地址", true
	}
	if containsAny(normalized, "请记住本站", "记住本站域名", "最新网址", "最新地址", "备用网址") &&
		containsAny(normalized, "本站", "网站", "网址", "域名") {
		return "疑似包含第三方网站引流信息", true
	}
	if containsAny(normalized, "本书来自", "内容来自", "转载自") &&
		containsAny(normalized, "小说网", "文学网", "阅读网", "网站") {
		return "疑似包含第三方网站来源广告", true
	}
	if containsAny(normalized, "关注公众号", "微信公众号") &&
		containsAny(normalized, "关注", "搜索", "回复", "领取", "获取") {
		return "疑似包含公众号引流信息", true
	}
	if containsAny(normalized, "qq群", "qq 群") &&
		containsAny(normalized, "加入", "加群", "读者群", "交流群", "领取") {
		return "疑似包含第三方群组引流信息", true
	}
	return "", false
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func defaultThirdPartyDetectors() []thirdPartyDetector {
	return []thirdPartyDetector{promotionCombinationDetector{}}
}

func detectThirdPartyIssues(report *PreflightReport, detectors []thirdPartyDetector, lineNumber int, chapterTitle, line string) {
	for _, detector := range detectors {
		message, matched := detector.Detect(line)
		if !matched {
			continue
		}
		report.addIssue(PreflightIssue{
			Type:         PreflightIssueThirdPartyAd,
			Severity:     PreflightSeverityWarning,
			LineNumber:   lineNumber,
			ChapterTitle: chapterTitle,
			Message:      message,
			Evidence:     line,
			Detector:     detector.Name(),
		})
		return
	}
}
