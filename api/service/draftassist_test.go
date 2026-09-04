package service

import (
	"reflect"
	"testing"
)

// P10 纯单测：草稿文本 → 段落切分（空行分段/硬换行拼接/markdown 标题前缀剥离/空段忽略）。

func TestParseDraftParagraphs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "空行分段+硬换行拼接",
			in:   "第一段第一句，\n仍是第一段（硬换行不该断段）。\n\n第二段整段只有一句。",
			want: []string{"第一段第一句，仍是第一段（硬换行不该断段）。", "第二段整段只有一句。"},
		},
		{
			name: "markdown 标题前缀剥离后并入正文",
			in:   "# 招生工作安排\n\n我校 2024 年招生工作自 3 月启动。",
			want: []string{"招生工作安排", "我校 2024 年招生工作自 3 月启动。"},
		},
		{
			name: "连续空行与首尾空行被忽略",
			in:   "\n\n  第一句。\n\n\n  第二句。\n\n",
			want: []string{"第一句。", "第二句。"},
		},
		{
			name: "无内容返回空",
			in:   "  \n\n  ",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDraftParagraphs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseDraftParagraphs(%q)\n got=%#v\nwant=%#v", tc.in, got, tc.want)
			}
		})
	}
}
