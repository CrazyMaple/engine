package inventory

import (
	"testing"
)

func setupRegistry() *TemplateRegistry {
	reg := NewTemplateRegistry()
	reg.Register(&ItemTemplate{ID: 1, Name: "HP Potion", Type: ItemTypeConsumable, MaxStack: 99})
	reg.Register(&ItemTemplate{ID: 2, Name: "MP Potion", Type: ItemTypeConsumable, MaxStack: 99})
	reg.Register(&ItemTemplate{ID: 3, Name: "Iron Sword", Type: ItemTypeEquipment, MaxStack: 1})
	reg.Register(&ItemTemplate{ID: 4, Name: "Wood", Type: ItemTypeMaterial, MaxStack: 999})
	reg.Register(&ItemTemplate{ID: 5, Name: "Gold", Type: ItemTypeCurrency, MaxStack: 99999})
	return reg
}

func TestAddItem_Basic(t *testing.T) {
	inv := NewInventory(10, setupRegistry())

	affected, err := inv.AddItem(1, 10)
	if err != nil {
		t.Fatalf("add item: %v", err)
	}
	if len(affected) != 1 {
		t.Errorf("expected 1 affected slot, got %d", len(affected))
	}
	if affected[0].Quantity != 10 {
		t.Errorf("expected quantity 10, got %d", affected[0].Quantity)
	}
	if inv.UsedSlots() != 1 {
		t.Errorf("expected 1 used slot, got %d", inv.UsedSlots())
	}
}

func TestAddItem_Stacking(t *testing.T) {
	inv := NewInventory(10, setupRegistry())

	inv.AddItem(1, 50) // HP Potion x50
	inv.AddItem(1, 30) // 应堆叠到已有的槽位

	if inv.UsedSlots() != 1 {
		t.Errorf("expected 1 slot (stacked), got %d", inv.UsedSlots())
	}
	if inv.CountByTemplate(1) != 80 {
		t.Errorf("expected 80 total, got %d", inv.CountByTemplate(1))
	}
}

func TestAddItem_StackOverflow(t *testing.T) {
	inv := NewInventory(10, setupRegistry())

	inv.AddItem(1, 99) // 填满一个槽位
	inv.AddItem(1, 50) // 需要新槽位

	if inv.UsedSlots() != 2 {
		t.Errorf("expected 2 slots, got %d", inv.UsedSlots())
	}
	if inv.CountByTemplate(1) != 149 {
		t.Errorf("expected 149 total, got %d", inv.CountByTemplate(1))
	}
}

func TestAddItem_NonStackable(t *testing.T) {
	inv := NewInventory(10, setupRegistry())

	affected, err := inv.AddItem(3, 3) // 3把剑，每把占一格
	if err != nil {
		t.Fatalf("add item: %v", err)
	}
	if len(affected) != 3 {
		t.Errorf("expected 3 affected slots, got %d", len(affected))
	}
	if inv.UsedSlots() != 3 {
		t.Errorf("expected 3 used slots, got %d", inv.UsedSlots())
	}
}

func TestAddItem_FullInventory(t *testing.T) {
	inv := NewInventory(2, setupRegistry())

	inv.AddItem(3, 2) // 填满
	_, err := inv.AddItem(3, 1)
	if err == nil {
		t.Error("expected error when inventory is full")
	}
}

func TestRemoveItem(t *testing.T) {
	inv := NewInventory(10, setupRegistry())
	affected, _ := inv.AddItem(1, 50)
	slot := affected[0].SlotIndex

	err := inv.RemoveItem(slot, 20)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if inv.CountByTemplate(1) != 30 {
		t.Errorf("expected 30 remaining, got %d", inv.CountByTemplate(1))
	}

	// 移除全部
	err = inv.RemoveItem(slot, 30)
	if err != nil {
		t.Fatalf("remove all: %v", err)
	}
	if inv.UsedSlots() != 0 {
		t.Errorf("expected 0 used slots, got %d", inv.UsedSlots())
	}
}

func TestRemoveByTemplate(t *testing.T) {
	inv := NewInventory(10, setupRegistry())

	inv.AddItem(1, 99)
	inv.AddItem(1, 50) // 两个槽位

	err := inv.RemoveByTemplate(1, 120)
	if err != nil {
		t.Fatalf("remove by template: %v", err)
	}
	if inv.CountByTemplate(1) != 29 {
		t.Errorf("expected 29 remaining, got %d", inv.CountByTemplate(1))
	}
}

func TestRemoveByTemplate_NotEnough(t *testing.T) {
	inv := NewInventory(10, setupRegistry())
	inv.AddItem(1, 10)

	err := inv.RemoveByTemplate(1, 20)
	if err == nil {
		t.Error("expected error when not enough items")
	}
}

func TestUseItem(t *testing.T) {
	inv := NewInventory(10, setupRegistry())
	affected, _ := inv.AddItem(1, 5)
	slot := affected[0].SlotIndex

	used, err := inv.UseItem(slot)
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if used.Quantity != 1 {
		t.Errorf("expected used quantity 1, got %d", used.Quantity)
	}
	if inv.CountByTemplate(1) != 4 {
		t.Errorf("expected 4 remaining, got %d", inv.CountByTemplate(1))
	}
}

func TestUseItem_NotConsumable(t *testing.T) {
	inv := NewInventory(10, setupRegistry())
	affected, _ := inv.AddItem(3, 1) // 装备
	slot := affected[0].SlotIndex

	_, err := inv.UseItem(slot)
	if err == nil {
		t.Error("expected error for non-consumable item")
	}
}

func TestSplitStack(t *testing.T) {
	inv := NewInventory(10, setupRegistry())
	affected, _ := inv.AddItem(1, 50)
	slot := affected[0].SlotIndex

	newSlot, err := inv.SplitStack(slot, 20)
	if err != nil {
		t.Fatalf("split: %v", err)
	}

	original, _ := inv.GetSlot(slot)
	split, _ := inv.GetSlot(newSlot)

	if original.Quantity != 30 {
		t.Errorf("original should have 30, got %d", original.Quantity)
	}
	if split.Quantity != 20 {
		t.Errorf("split should have 20, got %d", split.Quantity)
	}
}

func TestMergeStacks(t *testing.T) {
	inv := NewInventory(10, setupRegistry())

	// 创建两个未满的槽位：先加30，拆分出20，得到两个槽位（10和20）
	affected, _ := inv.AddItem(1, 30)
	slot1 := affected[0].SlotIndex
	slot2, err := inv.SplitStack(slot1, 20)
	if err != nil {
		t.Fatalf("split for merge test: %v", err)
	}

	err = inv.MergeStacks(slot2, slot1)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	merged, _ := inv.GetSlot(slot1)
	if merged.Quantity != 30 {
		t.Errorf("expected 30 after merge, got %d", merged.Quantity)
	}
	if inv.UsedSlots() != 1 {
		t.Errorf("expected 1 slot after merge, got %d", inv.UsedSlots())
	}
}

func TestSortByType(t *testing.T) {
	inv := NewInventory(10, setupRegistry())
	inv.AddItem(3, 1) // Equipment
	inv.AddItem(1, 5) // Consumable
	inv.AddItem(4, 10) // Material

	inv.SortByType()

	items := inv.AllItems()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// 验证排序后槽位连续
	for i, item := range items {
		if item.SlotIndex != i {
			t.Errorf("expected slot %d, got %d", i, item.SlotIndex)
		}
	}
}

func TestFindByTemplate(t *testing.T) {
	inv := NewInventory(10, setupRegistry())
	inv.AddItem(1, 50)
	inv.AddItem(2, 30)
	inv.AddItem(1, 20)

	stacks := inv.FindByTemplate(1)
	total := 0
	for _, s := range stacks {
		total += s.Quantity
	}
	if total != 70 {
		t.Errorf("expected 70 of template 1, got %d", total)
	}
}

func TestFreeSlots(t *testing.T) {
	inv := NewInventory(5, setupRegistry())
	if inv.FreeSlots() != 5 {
		t.Errorf("expected 5 free, got %d", inv.FreeSlots())
	}

	inv.AddItem(1, 10)
	if inv.FreeSlots() != 4 {
		t.Errorf("expected 4 free, got %d", inv.FreeSlots())
	}
}

func TestTemplateRegistry(t *testing.T) {
	reg := setupRegistry()

	tmpl, ok := reg.Get(1)
	if !ok || tmpl.Name != "HP Potion" {
		t.Error("failed to get template 1")
	}

	_, ok = reg.Get(999)
	if ok {
		t.Error("should not find template 999")
	}

	all := reg.All()
	if len(all) != 5 {
		t.Errorf("expected 5 templates, got %d", len(all))
	}
}

func TestItemTemplate_IsStackable(t *testing.T) {
	stackable := &ItemTemplate{MaxStack: 99}
	if !stackable.IsStackable() {
		t.Error("MaxStack=99 should be stackable")
	}

	notStackable := &ItemTemplate{MaxStack: 1}
	if notStackable.IsStackable() {
		t.Error("MaxStack=1 should not be stackable")
	}
}
