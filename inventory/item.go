package inventory

// ItemType 道具类型
type ItemType int

const (
	ItemTypeConsumable ItemType = iota // 消耗品
	ItemTypeEquipment                  // 装备
	ItemTypeMaterial                   // 材料
	ItemTypeCurrency                   // 货币
)

func (t ItemType) String() string {
	switch t {
	case ItemTypeConsumable:
		return "consumable"
	case ItemTypeEquipment:
		return "equipment"
	case ItemTypeMaterial:
		return "material"
	case ItemTypeCurrency:
		return "currency"
	default:
		return "unknown"
	}
}

// ItemTemplate 道具模板（配表定义，不可变）
type ItemTemplate struct {
	ID         int                    // 道具模板 ID
	Name       string                 // 道具名称
	Type       ItemType               // 道具类型
	MaxStack   int                    // 最大堆叠数（1 = 不可堆叠）
	Properties map[string]interface{} // 属性效果（攻击力、生命值等）
}

// IsStackable 是否可堆叠
func (t *ItemTemplate) IsStackable() bool {
	return t.MaxStack > 1
}

// ItemStack 道具堆叠实例（背包中的一个槽位）
type ItemStack struct {
	TemplateID int                    // 道具模板 ID
	Quantity   int                    // 数量
	SlotIndex  int                    // 槽位索引
	Metadata   map[string]interface{} // 实例数据（强化等级、耐久等）
}

// TemplateRegistry 道具模板注册表
type TemplateRegistry struct {
	templates map[int]*ItemTemplate
}

// NewTemplateRegistry 创建模板注册表
func NewTemplateRegistry() *TemplateRegistry {
	return &TemplateRegistry{
		templates: make(map[int]*ItemTemplate),
	}
}

// Register 注册道具模板
func (r *TemplateRegistry) Register(tmpl *ItemTemplate) {
	r.templates[tmpl.ID] = tmpl
}

// Get 获取道具模板
func (r *TemplateRegistry) Get(id int) (*ItemTemplate, bool) {
	t, ok := r.templates[id]
	return t, ok
}

// All 获取所有模板
func (r *TemplateRegistry) All() []*ItemTemplate {
	result := make([]*ItemTemplate, 0, len(r.templates))
	for _, t := range r.templates {
		result = append(result, t)
	}
	return result
}
