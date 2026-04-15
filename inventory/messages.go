package inventory

// --- Actor 消息定义（用于通过 Actor 消息模式操作背包）---

// AddItemRequest 添加道具请求
type AddItemRequest struct {
	TemplateID int
	Quantity   int
}

// AddItemResponse 添加道具响应
type AddItemResponse struct {
	Success  bool
	Affected []ItemStack
	Error    string
}

// RemoveItemRequest 移除道具请求
type RemoveItemRequest struct {
	SlotIndex int
	Quantity  int
}

// RemoveItemResponse 移除道具响应
type RemoveItemResponse struct {
	Success bool
	Error   string
}

// UseItemRequest 使用道具请求
type UseItemRequest struct {
	SlotIndex int
}

// UseItemResponse 使用道具响应
type UseItemResponse struct {
	Success bool
	Used    *ItemStack
	Error   string
}

// SplitStackRequest 拆分堆叠请求
type SplitStackRequest struct {
	SlotIndex int
	Quantity  int
}

// SplitStackResponse 拆分堆叠响应
type SplitStackResponse struct {
	Success  bool
	NewSlot  int
	Error    string
}

// MergeStacksRequest 合并堆叠请求
type MergeStacksRequest struct {
	FromSlot int
	ToSlot   int
}

// MergeStacksResponse 合并堆叠响应
type MergeStacksResponse struct {
	Success bool
	Error   string
}

// SortRequest 整理背包请求
type SortRequest struct{}

// QueryItemsRequest 查询背包请求
type QueryItemsRequest struct {
	TemplateID *int // nil 表示查询所有
}

// QueryItemsResponse 查询背包响应
type QueryItemsResponse struct {
	Items    []ItemStack
	UsedSlots int
	Capacity  int
}
