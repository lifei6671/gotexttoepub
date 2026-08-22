package webapp

import (
	"context"
	"encoding/xml"
	"os"
	"strings"
	"testing"
)

func TestBuildAutoCoverSVGUsesRecognizedAuthorAndEscapesText(t *testing.T) {
	svg, err := buildAutoCoverSVG("测试 <标题>", "张&三")
	if err != nil {
		t.Fatalf("buildAutoCoverSVG() error = %v", err)
	}
	if err := xml.Unmarshal([]byte(svg), new(any)); err != nil {
		t.Fatalf("generated SVG is not valid XML: %v", err)
	}
	for _, want := range []string{"测试 &lt;标题&gt;", "作者：张&amp;三"} {
		if !strings.Contains(svg, want) {
			t.Fatalf("generated SVG does not contain %q:\n%s", want, svg)
		}
	}
}

func TestBuildAutoCoverSVGUsesFallbackAuthor(t *testing.T) {
	svg, err := buildAutoCoverSVG("测试小说", " \t\n")
	if err != nil {
		t.Fatalf("buildAutoCoverSVG() error = %v", err)
	}
	if !strings.Contains(svg, "作者：未署名") {
		t.Fatalf("generated SVG does not contain fallback author:\n%s", svg)
	}
}

func TestAutoCoverGeneratorCreatesCleanedUpSVG(t *testing.T) {
	generator := newAutoCoverGenerator(t.TempDir())
	path, cleanup, err := generator(context.Background(), "测试小说", "测试作者")
	if err != nil {
		t.Fatalf("generator() error = %v", err)
	}
	if !strings.HasSuffix(path, ".svg") {
		t.Fatalf("generated path = %q, want .svg", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated SVG: %v", err)
	}
	if !strings.Contains(string(content), "作者：测试作者") {
		t.Fatalf("generated SVG did not contain author: %s", content)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("generated SVG stat error = %v, want not exist", err)
	}
}
