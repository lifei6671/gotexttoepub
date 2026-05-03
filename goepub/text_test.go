package goepub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func TestDecodeTextContentAutoGB18030(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	encoded, _, err := transform.String(simplifiedchinese.GB18030.NewEncoder(), "我不是戏神 作者：三九音域")
	if err != nil {
		t.Fatalf("encode gb18030: %v", err)
	}

	decoded, detectedEncoding, err := decodeTextContent([]byte(encoded), "")
	if err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if detectedEncoding != encodingGB18030 {
		t.Fatalf("expected encoding %s, got %s", encodingGB18030, detectedEncoding)
	}
	if decoded != "我不是戏神 作者：三九音域" {
		t.Fatalf("unexpected decoded content: %s", decoded)
	}
}

func TestParseExtractsInlineMetadataAndPrefixedIntro(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	tmpDir := t.TempDir()
	txtPath := filepath.Join(tmpDir, "gbk-story.txt")
	outputPath := filepath.Join(tmpDir, "story.epub")

	source := strings.Join([]string{
		"我不是戏神 作者：三九音域",
		"",
		"书籍简介：赤色流星划过天际后，人类文明陷入停滞。",
		"从那天起，人们再也无法制造一枚火箭。",
		"",
		"第1章 戏鬼回家",
		"我是谁？",
	}, "\n")

	encoded, _, err := transform.String(simplifiedchinese.GBK.NewEncoder(), source)
	if err != nil {
		t.Fatalf("encode gbk: %v", err)
	}
	if err := os.WriteFile(txtPath, []byte(encoded), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	book := &Book{
		Filename: txtPath,
		Output:   outputPath,
	}
	if err := NewEPUBConverter().Convert(context.Background(), book); err != nil {
		t.Fatalf("convert: %v", err)
	}

	if book.Name != "我不是戏神" {
		t.Fatalf("unexpected title: %s", book.Name)
	}
	if book.Author != "三九音域" {
		t.Fatalf("unexpected author: %s", book.Author)
	}
	if !strings.Contains(book.Intro, "人类文明陷入停滞") {
		t.Fatalf("unexpected intro: %s", book.Intro)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected epub output: %v", err)
	}
}

func TestVolumePatternDoesNotTreatAuthorNotesAsRealVolumes(t *testing.T) {
	isolateAutoRuleConfigDiscovery(t)

	rules, err := compileRuleConfig(defaultRuleConfig())
	if err != nil {
		t.Fatalf("compile default rules: %v", err)
	}

	falsePositive := "第一卷接近尾声，三九要再整理一遍后面的大纲，今天只有两更~"
	if rules.VolumeRegex.MatchString(falsePositive) {
		t.Fatalf("expected line not to be treated as a real volume title: %s", falsePositive)
	}

	realVolume := "第一卷 灰界降临"
	if !rules.VolumeRegex.MatchString(realVolume) {
		t.Fatalf("expected line to be treated as a real volume title: %s", realVolume)
	}
}
