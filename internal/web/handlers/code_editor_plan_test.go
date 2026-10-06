package handlers

import (
	"strings"
	"testing"
)

const fence = "```"

func TestExtractAtlantisPlan_SingleProject(t *testing.T) {
	body := "Ran Plan for dir: `tms` workspace: `default`\n\n<details><summary>Show Output</summary>\n\n" +
		fence + "diff\n~ resource \"azurerm_kubernetes_cluster\" \"aks\" {\n      ~ sku_tier = \"Free\" -> \"Standard\"\n    }\n\nPlan: 0 to add, 1 to change, 0 to destroy.\n" + fence +
		"\n\n* :arrow_forward: To **apply** this plan, comment:\n</details>\nPlan: 0 to add, 1 to change, 0 to destroy."
	plan := extractAtlantisPlan([]ghIssueComment{{Body: "lgtm"}, {Body: body}, {Body: "outro comentário"}})
	if plan == nil || len(plan.Projects) != 1 {
		t.Fatalf("esperava 1 projeto, veio %+v", plan)
	}
	p := plan.Projects[0]
	if p.Label != "dir: tms workspace: default" {
		t.Errorf("label = %q", p.Label)
	}
	if !strings.HasPrefix(p.Content, "~ resource") || strings.Contains(p.Content, fence) {
		t.Errorf("content inesperado: %q", p.Content)
	}
	if p.Summary != "Plan: 0 to add, 1 to change, 0 to destroy." {
		t.Errorf("summary = %q", p.Summary)
	}
}

func TestExtractAtlantisPlan_MultiProjectUsesLatest(t *testing.T) {
	old := "Ran Plan for dir: `velho` workspace: `default`\n\n" + fence + "diff\nvelho\n" + fence + "\n"
	body := "Ran Plan for 2 projects:\n\n1. dir: `a` workspace: `default`\n1. dir: `b` workspace: `default`\n\n" +
		"### 1. dir: `a` workspace: `default`\n<details><summary>Show Output</summary>\n\n" + fence + "diff\n+ add a\nPlan: 1 to add, 0 to change, 0 to destroy.\n" + fence + "\n</details>\n\n---\n" +
		"### 2. dir: `b` workspace: `default`\n**Plan Error**\n" + fence + "\nerro b\n" + fence + "\n"
	plan := extractAtlantisPlan([]ghIssueComment{{Body: old}, {Body: body}})
	if plan == nil || len(plan.Projects) != 2 {
		t.Fatalf("esperava 2 projetos, veio %+v", plan)
	}
	if plan.Projects[0].Label != "dir: a workspace: default" || plan.Projects[0].Content != "+ add a\nPlan: 1 to add, 0 to change, 0 to destroy." {
		t.Errorf("projeto a = %+v", plan.Projects[0])
	}
	if !plan.Projects[1].Error || plan.Projects[1].Content != "erro b" {
		t.Errorf("projeto b = %+v", plan.Projects[1])
	}
}

func TestExtractAtlantisPlan_JoinsSplitComments(t *testing.T) {
	part1 := "Ran Plan for dir: `tms` workspace: `default`\n\n<details><summary>Show Output</summary>\n\n" + fence + "diff\n+ resource \"x\" \"y\" {\n  + name = \"meio-da-li" +
		"\n" + fence + "\n</details>\n<br>\n\n**Warning**: Output length greater than max comment size. Continued in next comment."
	part2 := "Continued plan output from previous comment.\n<details><summary>Show Output</summary>\n\n" + fence + "diff\nnha\"\n}\nPlan: 1 to add, 0 to change, 0 to destroy.\n" + fence + "\n</details>"
	plan := extractAtlantisPlan([]ghIssueComment{{Body: part1}, {Body: part2}, {Body: "comentário humano"}})
	if plan == nil || len(plan.Projects) != 1 || plan.Comments != 2 {
		t.Fatalf("esperava 1 projeto em 2 comentários, veio %+v", plan)
	}
	want := "+ resource \"x\" \"y\" {\n  + name = \"meio-da-linha\"\n}\nPlan: 1 to add, 0 to change, 0 to destroy."
	if plan.Projects[0].Content != want {
		t.Errorf("content =\n%s\nwant\n%s", plan.Projects[0].Content, want)
	}
}

func TestExtractAtlantisPlan_NoPlan(t *testing.T) {
	if plan := extractAtlantisPlan([]ghIssueComment{{Body: "atlantis plan"}}); plan != nil {
		t.Errorf("esperava nil, veio %+v", plan)
	}
}
