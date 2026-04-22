package quest

// --- 分支任务（根据玩家选择走不同步骤） ---

// BranchDef 分支任务定义（扩展 QuestDef，用在 QuestDef.Branches 字段）
// 一个分支任务由一个"选择点"(ChoicePoint) 和多条 Path 组成。
// 触发选择点时，玩家通过 ChooseBranch 选择一条分支，之后 Steps 从 Path 中取用。
type BranchDef struct {
	// ChoicePoint 选择点 ID；配合 StepDef 的 EventType 等字段发出选择事件
	ChoicePoint string
	// Paths Key=分支 ID；Value=该分支的后续步骤
	Paths map[string][]StepDef
	// AutoCompleteOnPick 选择分支后是否把 ChoicePoint 步骤标记为已完成
	AutoCompleteOnPick bool
}

// BranchState 分支任务运行时状态
type BranchState struct {
	// Chosen 已选择的分支 ID（空字符串 = 未选择）
	Chosen string
	// PendingSteps 还未展开到 QuestInstance.Steps 的分支步骤
	PendingSteps []StepDef
}

// QuestDefBranch 嵌套在 QuestDef 上的分支定义（非破坏性扩展）
// 使用方式：给 QuestDef.Branches 赋值；QuestInstance.Branch 会被初始化。
//
// 为了不破坏现有 QuestDef，我们在 quest.go 之外通过访问器函数读取，
// 老代码无感。
type QuestDefBranch = BranchDef

// ChooseBranch 在 QuestInstance 上选择一条分支
// 返回 true 表示分支生效（追加了对应 Path 的步骤）。
func (q *QuestInstance) ChooseBranch(branchID string) bool {
	if q.Branch == nil || q.Def.Branches == nil {
		return false
	}
	if q.Branch.Chosen != "" {
		return false // 不允许二次选择
	}
	path, ok := q.Def.Branches.Paths[branchID]
	if !ok {
		return false
	}
	q.Branch.Chosen = branchID

	// 先将 ChoicePoint 对应步骤标记完成（如果配置了 AutoCompleteOnPick）
	if q.Def.Branches.AutoCompleteOnPick {
		for i, step := range q.Steps {
			if step.StepID == q.Def.Branches.ChoicePoint && !step.Done {
				q.Steps[i].Current = q.Steps[i].Required
				q.Steps[i].Done = true
			}
		}
	}

	// 将分支步骤展开到 Steps 与 Def.Steps
	for _, sd := range path {
		q.Def.Steps = append(q.Def.Steps, sd)
		q.Steps = append(q.Steps, StepProgress{
			StepID:   sd.ID,
			Required: sd.Required,
		})
	}
	q.Branch.PendingSteps = nil

	// 重新评估状态（选择可能已完成所有必要步骤）
	if q.allStepsDone() {
		q.Status = QuestCompleted
	}
	return true
}

// ChosenBranch 查询已选择的分支；未选择返回空
func (q *QuestInstance) ChosenBranch() string {
	if q.Branch == nil {
		return ""
	}
	return q.Branch.Chosen
}
