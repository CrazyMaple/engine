package inventory

import (
	"fmt"
	"sort"
)

// Inventory 背包
type Inventory struct {
	capacity int
	slots    map[int]*ItemStack     // slotIndex → ItemStack
	registry *TemplateRegistry
	nextSlot int                    // 下一个可用槽位
}

// NewInventory 创建背包
func NewInventory(capacity int, registry *TemplateRegistry) *Inventory {
	return &Inventory{
		capacity: capacity,
		slots:    make(map[int]*ItemStack),
		registry: registry,
	}
}

// AddItem 添加道具，自动堆叠或分配新槽位
// 返回受影响的槽位列表
func (inv *Inventory) AddItem(templateID int, quantity int) ([]ItemStack, error) {
	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}

	tmpl, ok := inv.registry.Get(templateID)
	if !ok {
		return nil, fmt.Errorf("unknown template ID: %d", templateID)
	}

	var affected []ItemStack
	remaining := quantity

	// 先尝试堆叠到已有的同类槽位
	if tmpl.MaxStack > 1 {
		for _, stack := range inv.slots {
			if stack.TemplateID != templateID || remaining <= 0 {
				continue
			}
			canAdd := tmpl.MaxStack - stack.Quantity
			if canAdd <= 0 {
				continue
			}
			add := remaining
			if add > canAdd {
				add = canAdd
			}
			stack.Quantity += add
			remaining -= add
			affected = append(affected, *stack)
		}
	}

	// 剩余数量分配新槽位
	for remaining > 0 {
		if len(inv.slots) >= inv.capacity {
			return affected, fmt.Errorf("inventory full, %d items not added", remaining)
		}

		add := remaining
		if add > tmpl.MaxStack {
			add = tmpl.MaxStack
		}

		slot := inv.findFreeSlot()
		stack := &ItemStack{
			TemplateID: templateID,
			Quantity:   add,
			SlotIndex:  slot,
		}
		inv.slots[slot] = stack
		remaining -= add
		affected = append(affected, *stack)
	}

	return affected, nil
}

// RemoveItem 从指定槽位移除道具
func (inv *Inventory) RemoveItem(slotIndex int, quantity int) error {
	stack, ok := inv.slots[slotIndex]
	if !ok {
		return fmt.Errorf("slot %d is empty", slotIndex)
	}
	if quantity <= 0 {
		return fmt.Errorf("quantity must be positive")
	}
	if quantity > stack.Quantity {
		return fmt.Errorf("slot %d has %d items, cannot remove %d", slotIndex, stack.Quantity, quantity)
	}

	stack.Quantity -= quantity
	if stack.Quantity == 0 {
		delete(inv.slots, slotIndex)
	}
	return nil
}

// RemoveByTemplate 按模板 ID 移除指定数量的道具（从多个槽位扣减）
func (inv *Inventory) RemoveByTemplate(templateID int, quantity int) error {
	if quantity <= 0 {
		return fmt.Errorf("quantity must be positive")
	}

	// 先检查总数是否够
	total := inv.CountByTemplate(templateID)
	if total < quantity {
		return fmt.Errorf("need %d items of template %d, only have %d", quantity, templateID, total)
	}

	remaining := quantity
	for slot, stack := range inv.slots {
		if stack.TemplateID != templateID || remaining <= 0 {
			continue
		}
		remove := remaining
		if remove > stack.Quantity {
			remove = stack.Quantity
		}
		stack.Quantity -= remove
		remaining -= remove
		if stack.Quantity == 0 {
			delete(inv.slots, slot)
		}
	}
	return nil
}

// UseItem 使用指定槽位的道具（扣减 1 个，返回使用的 ItemStack 副本）
func (inv *Inventory) UseItem(slotIndex int) (*ItemStack, error) {
	stack, ok := inv.slots[slotIndex]
	if !ok {
		return nil, fmt.Errorf("slot %d is empty", slotIndex)
	}

	tmpl, ok := inv.registry.Get(stack.TemplateID)
	if !ok {
		return nil, fmt.Errorf("template %d not found", stack.TemplateID)
	}
	if tmpl.Type != ItemTypeConsumable {
		return nil, fmt.Errorf("item %q is not consumable", tmpl.Name)
	}

	used := *stack
	used.Quantity = 1

	stack.Quantity--
	if stack.Quantity == 0 {
		delete(inv.slots, slotIndex)
	}

	return &used, nil
}

// SplitStack 拆分堆叠（从 fromSlot 拆分 quantity 个到新槽位）
func (inv *Inventory) SplitStack(fromSlot int, quantity int) (int, error) {
	stack, ok := inv.slots[fromSlot]
	if !ok {
		return -1, fmt.Errorf("slot %d is empty", fromSlot)
	}
	if quantity <= 0 || quantity >= stack.Quantity {
		return -1, fmt.Errorf("invalid split quantity %d (current: %d)", quantity, stack.Quantity)
	}
	if len(inv.slots) >= inv.capacity {
		return -1, fmt.Errorf("inventory full, cannot split")
	}

	newSlot := inv.findFreeSlot()
	newStack := &ItemStack{
		TemplateID: stack.TemplateID,
		Quantity:   quantity,
		SlotIndex:  newSlot,
	}
	if stack.Metadata != nil {
		newStack.Metadata = make(map[string]interface{}, len(stack.Metadata))
		for k, v := range stack.Metadata {
			newStack.Metadata[k] = v
		}
	}

	stack.Quantity -= quantity
	inv.slots[newSlot] = newStack

	return newSlot, nil
}

// MergeStacks 合并同类堆叠
func (inv *Inventory) MergeStacks(fromSlot, toSlot int) error {
	from, ok := inv.slots[fromSlot]
	if !ok {
		return fmt.Errorf("from slot %d is empty", fromSlot)
	}
	to, ok := inv.slots[toSlot]
	if !ok {
		return fmt.Errorf("to slot %d is empty", toSlot)
	}
	if from.TemplateID != to.TemplateID {
		return fmt.Errorf("cannot merge different item types")
	}

	tmpl, _ := inv.registry.Get(to.TemplateID)
	canAdd := tmpl.MaxStack - to.Quantity
	if canAdd <= 0 {
		return fmt.Errorf("target slot is full")
	}

	move := from.Quantity
	if move > canAdd {
		move = canAdd
	}

	to.Quantity += move
	from.Quantity -= move
	if from.Quantity == 0 {
		delete(inv.slots, fromSlot)
	}

	return nil
}

// SortByType 按道具类型排序整理背包
func (inv *Inventory) SortByType() {
	// 收集所有道具
	stacks := make([]*ItemStack, 0, len(inv.slots))
	for _, s := range inv.slots {
		stacks = append(stacks, s)
	}

	// 按类型和模板 ID 排序
	sort.Slice(stacks, func(i, j int) bool {
		ti, _ := inv.registry.Get(stacks[i].TemplateID)
		tj, _ := inv.registry.Get(stacks[j].TemplateID)
		if ti != nil && tj != nil && ti.Type != tj.Type {
			return ti.Type < tj.Type
		}
		return stacks[i].TemplateID < stacks[j].TemplateID
	})

	// 重新分配槽位
	inv.slots = make(map[int]*ItemStack, len(stacks))
	for i, s := range stacks {
		s.SlotIndex = i
		inv.slots[i] = s
	}
	inv.nextSlot = len(stacks)
}

// FindByTemplate 按模板 ID 查找所有槽位
func (inv *Inventory) FindByTemplate(templateID int) []ItemStack {
	var result []ItemStack
	for _, s := range inv.slots {
		if s.TemplateID == templateID {
			result = append(result, *s)
		}
	}
	return result
}

// CountByTemplate 统计指定模板的道具总数
func (inv *Inventory) CountByTemplate(templateID int) int {
	total := 0
	for _, s := range inv.slots {
		if s.TemplateID == templateID {
			total += s.Quantity
		}
	}
	return total
}

// GetSlot 获取指定槽位
func (inv *Inventory) GetSlot(slotIndex int) (*ItemStack, bool) {
	s, ok := inv.slots[slotIndex]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

// AllItems 获取所有道具（槽位快照）
func (inv *Inventory) AllItems() []ItemStack {
	result := make([]ItemStack, 0, len(inv.slots))
	for _, s := range inv.slots {
		result = append(result, *s)
	}
	return result
}

// UsedSlots 已使用槽位数
func (inv *Inventory) UsedSlots() int {
	return len(inv.slots)
}

// Capacity 背包容量
func (inv *Inventory) Capacity() int {
	return inv.capacity
}

// FreeSlots 剩余空闲槽位数
func (inv *Inventory) FreeSlots() int {
	return inv.capacity - len(inv.slots)
}

func (inv *Inventory) findFreeSlot() int {
	for {
		if _, ok := inv.slots[inv.nextSlot]; !ok {
			slot := inv.nextSlot
			inv.nextSlot++
			return slot
		}
		inv.nextSlot++
	}
}
