package skill

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSkillsFromRecordFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills.txt")
	// 表头 + 2 行：fireball / heal
	content := "ID\tName\tLevel\tCooldownMS\tGlobalCDMS\tCastTimeMS\tBackSwingMS\tTargetType\tRange\tAOERadius\tCostType\tCostValue\tEffects\tTags\tDescription\n" +
		"fireball\t火球术\t1\t3000\t500\t1000\t200\t1\t30\t0\t1\t20\tfire_dmg\tmagic|fire\t发射火球\n" +
		"heal\t治疗\t1\t5000\t500\t1500\t300\t1\t20\t0\t1\t15\theal_eff\tholy\t恢复生命\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	reg := NewSkillRegistry()
	n, err := LoadSkillsFromRecordFile(path, reg)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 skills, got %d", n)
	}

	fb, ok := reg.Get("fireball")
	if !ok {
		t.Fatal("fireball not registered")
	}
	if fb.Cooldown != 3*time.Second {
		t.Errorf("cooldown want 3s, got %v", fb.Cooldown)
	}
	if fb.TargetType != TargetSingle {
		t.Errorf("target type want Single, got %v", fb.TargetType)
	}
	if len(fb.Effects) != 1 || fb.Effects[0] != "fire_dmg" {
		t.Errorf("effects mismatch: %v", fb.Effects)
	}
	if len(fb.Tags) != 2 || fb.Tags[0] != "magic" || fb.Tags[1] != "fire" {
		t.Errorf("tags mismatch: %v", fb.Tags)
	}
}

func TestLoadBuffsFromRecordFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "buffs.txt")
	// 用 csv.Writer 生成以确保 JSON 中的双引号被正确转义
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	w.Comma = '\t'
	rows := [][]string{
		{"ID", "Name", "Category", "DurationMS", "MaxStack", "StackPolicy", "TickInterval", "Priority", "MutexGroup", "Tags", "Modifiers", "TickDamage", "TickHeal"},
		{"burn", "燃烧", "1", "6000", "5", "1", "1000", "10", "dot", "fire|dot", "[]", "10", "0"},
		{"haste", "急速", "0", "8000", "1", "0", "0", "10", "move", "buff", `[{"Attribute":"speed","AddValue":0,"MulValue":0.3}]`, "0", "0"},
	}
	if err := w.WriteAll(rows); err != nil {
		t.Fatal(err)
	}
	f.Close()

	reg := NewBuffRegistry()
	n, err := LoadBuffsFromRecordFile(path, reg)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 buffs, got %d", n)
	}
	burn, ok := reg.Get("burn")
	if !ok {
		t.Fatal("burn not registered")
	}
	if burn.Duration != 6*time.Second {
		t.Errorf("duration want 6s, got %v", burn.Duration)
	}
	if burn.TickInterval != time.Second {
		t.Errorf("tick interval want 1s, got %v", burn.TickInterval)
	}
	if burn.TickDamage != 10 {
		t.Errorf("tick dmg want 10, got %v", burn.TickDamage)
	}

	haste, _ := reg.Get("haste")
	if len(haste.Modifiers) != 1 {
		t.Fatalf("haste modifiers want 1, got %d", len(haste.Modifiers))
	}
	m := haste.Modifiers[0]
	if m.Attribute != "speed" || m.MulValue != 0.3 {
		t.Errorf("modifier mismatch: %+v", m)
	}
}

func TestLoadSkillsFromJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills.json")
	content := `[
	  {"id":"strike","name":"强击","level":1,"cooldown_ms":2000,"target_type":1,"range":5,"effects":["phys_dmg"],"tags":["physical"],"description":"近战伤害"},
	  {"id":"dash","name":"冲刺","level":1,"cooldown_ms":1000,"target_type":3,"effects":["move_buff"]}
	]`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	reg := NewSkillRegistry()
	n, err := LoadSkillsFromJSON(path, reg)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2, got %d", n)
	}
	s, ok := reg.Get("strike")
	if !ok || s.Cooldown != 2*time.Second {
		t.Errorf("strike cooldown wrong: %+v", s)
	}
}

func TestLoadBuffsFromJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "buffs.json")
	content := `[
	  {"id":"poison","name":"毒","category":1,"duration_ms":5000,"max_stack":3,"stack_policy":1,"tick_interval_ms":1000,"tick_damage":5}
	]`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	reg := NewBuffRegistry()
	n, err := LoadBuffsFromJSON(path, reg)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1, got %d", n)
	}
	p, ok := reg.Get("poison")
	if !ok {
		t.Fatal("poison not registered")
	}
	if p.TickInterval != time.Second || p.TickDamage != 5 {
		t.Errorf("poison params wrong: %+v", p)
	}
	if p.MaxStack != 3 {
		t.Errorf("poison max stack want 3, got %d", p.MaxStack)
	}
}

func TestSplitList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a|b|c", []string{"a", "b", "c"}},
		{"a | b ", []string{"a", "b"}},
		{`["x","y"]`, []string{"x", "y"}},
	}
	for _, c := range cases {
		got := splitList(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitList(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitList(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestLoader_EmptyIDRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.txt")
	content := "ID\tName\tLevel\tCooldownMS\tGlobalCDMS\tCastTimeMS\tBackSwingMS\tTargetType\tRange\tAOERadius\tCostType\tCostValue\tEffects\tTags\tDescription\n" +
		"\t空ID\t1\t1000\t0\t0\t0\t0\t0\t0\t0\t0\t\t\t\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	reg := NewSkillRegistry()
	_, err := LoadSkillsFromRecordFile(path, reg)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}
