package scene

import "math"

// CrossLinkedListAOI 十字链表 AOI
// X 轴和 Y 轴各维护一条按坐标排序的双向链表
// 查询时沿两条链表交叉扫描，取交集即为 AOI 范围内的实体
// 优点：移动时只需局部调整链表，适合实体密集且频繁移动的场景
type CrossLinkedListAOI struct {
	width, height float32
	viewRange     float32
	xHead         *crossNode // X 轴链表哨兵头
	xTail         *crossNode // X 轴链表哨兵尾
	yHead         *crossNode // Y 轴链表哨兵头
	yTail         *crossNode // Y 轴链表哨兵尾
	entities      map[string]*crossNode
}

// crossNode 十字链表节点
type crossNode struct {
	entity *GridEntity
	xPrev  *crossNode
	xNext  *crossNode
	yPrev  *crossNode
	yNext  *crossNode
}

// NewCrossLinkedListAOI 创建十字链表 AOI
func NewCrossLinkedListAOI(width, height, viewRange float32) *CrossLinkedListAOI {
	// 创建哨兵节点
	xHead := &crossNode{entity: &GridEntity{X: -math.MaxFloat32}}
	xTail := &crossNode{entity: &GridEntity{X: math.MaxFloat32}}
	xHead.xNext = xTail
	xTail.xPrev = xHead

	yHead := &crossNode{entity: &GridEntity{Y: -math.MaxFloat32}}
	yTail := &crossNode{entity: &GridEntity{Y: math.MaxFloat32}}
	yHead.yNext = yTail
	yTail.yPrev = yHead

	return &CrossLinkedListAOI{
		width:     width,
		height:    height,
		viewRange: viewRange,
		xHead:     xHead,
		xTail:     xTail,
		yHead:     yHead,
		yTail:     yTail,
		entities:  make(map[string]*crossNode),
	}
}

func (c *CrossLinkedListAOI) Add(entity *GridEntity) {
	if _, exists := c.entities[entity.ID]; exists {
		return
	}
	node := &crossNode{entity: entity}
	c.entities[entity.ID] = node

	// 插入 X 轴链表（按 X 坐标有序）
	c.insertX(node)
	// 插入 Y 轴链表（按 Y 坐标有序）
	c.insertY(node)
}

func (c *CrossLinkedListAOI) Remove(entityID string) *GridEntity {
	node, ok := c.entities[entityID]
	if !ok {
		return nil
	}
	delete(c.entities, entityID)

	// 从 X 轴链表移除
	node.xPrev.xNext = node.xNext
	node.xNext.xPrev = node.xPrev

	// 从 Y 轴链表移除
	node.yPrev.yNext = node.yNext
	node.yNext.yPrev = node.yPrev

	return node.entity
}

func (c *CrossLinkedListAOI) Get(entityID string) *GridEntity {
	if node, ok := c.entities[entityID]; ok {
		return node.entity
	}
	return nil
}

func (c *CrossLinkedListAOI) Move(entityID string, newX, newY float32) (entered, left []*GridEntity) {
	node, ok := c.entities[entityID]
	if !ok {
		return nil, nil
	}

	oldX, oldY := node.entity.X, node.entity.Y

	// 获取移动前的 AOI 集合
	oldNearby := c.nearbySet(node)

	// 更新坐标
	node.entity.X = newX
	node.entity.Y = newY

	// 调整 X 轴链表位置
	c.adjustX(node, oldX)
	// 调整 Y 轴链表位置
	c.adjustY(node, oldY)

	// 获取移动后的 AOI 集合
	newNearby := c.nearbySet(node)

	// 计算进入/离开
	for id, e := range newNearby {
		if _, inOld := oldNearby[id]; !inOld {
			entered = append(entered, e)
		}
	}
	for id, e := range oldNearby {
		if _, inNew := newNearby[id]; !inNew {
			left = append(left, e)
		}
	}

	return entered, left
}

func (c *CrossLinkedListAOI) GetNearby(entityID string) []*GridEntity {
	node, ok := c.entities[entityID]
	if !ok {
		return nil
	}

	nearby := c.nearbySet(node)
	result := make([]*GridEntity, 0, len(nearby))
	for _, e := range nearby {
		result = append(result, e)
	}
	return result
}

func (c *CrossLinkedListAOI) EntityCount() int {
	return len(c.entities)
}

// insertX 按 X 坐标有序插入到 X 轴链表
func (c *CrossLinkedListAOI) insertX(node *crossNode) {
	cur := c.xHead.xNext
	for cur != c.xTail && cur.entity.X < node.entity.X {
		cur = cur.xNext
	}
	// 插入到 cur 前面
	node.xPrev = cur.xPrev
	node.xNext = cur
	cur.xPrev.xNext = node
	cur.xPrev = node
}

// insertY 按 Y 坐标有序插入到 Y 轴链表
func (c *CrossLinkedListAOI) insertY(node *crossNode) {
	cur := c.yHead.yNext
	for cur != c.yTail && cur.entity.Y < node.entity.Y {
		cur = cur.yNext
	}
	node.yPrev = cur.yPrev
	node.yNext = cur
	cur.yPrev.yNext = node
	cur.yPrev = node
}

// adjustX 在 X 坐标变化后调整链表位置
func (c *CrossLinkedListAOI) adjustX(node *crossNode, oldX float32) {
	newX := node.entity.X
	if newX == oldX {
		return
	}

	// 先从当前位置摘出
	node.xPrev.xNext = node.xNext
	node.xNext.xPrev = node.xPrev

	// 重新插入
	c.insertX(node)
}

// adjustY 在 Y 坐标变化后调整链表位置
func (c *CrossLinkedListAOI) adjustY(node *crossNode, oldY float32) {
	newY := node.entity.Y
	if newY == oldY {
		return
	}

	node.yPrev.yNext = node.yNext
	node.yNext.yPrev = node.yPrev

	c.insertY(node)
}

// nearbySet 获取节点 AOI 范围内的实体集合（沿 X 轴链表扫描，距离过滤）
func (c *CrossLinkedListAOI) nearbySet(node *crossNode) map[string]*GridEntity {
	result := make(map[string]*GridEntity)
	vr := c.viewRange
	x, y := node.entity.X, node.entity.Y

	// 向右扫描 X 轴链表
	for cur := node.xNext; cur != c.xTail; cur = cur.xNext {
		dx := cur.entity.X - x
		if dx > vr {
			break // X 差距已超过视野，后续更大
		}
		dy := cur.entity.Y - y
		if dy >= -vr && dy <= vr {
			result[cur.entity.ID] = cur.entity
		}
	}

	// 向左扫描 X 轴链表
	for cur := node.xPrev; cur != c.xHead; cur = cur.xPrev {
		dx := x - cur.entity.X
		if dx > vr {
			break
		}
		dy := cur.entity.Y - y
		if dy >= -vr && dy <= vr {
			result[cur.entity.ID] = cur.entity
		}
	}

	return result
}
