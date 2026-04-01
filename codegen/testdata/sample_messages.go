package testdata

//msggen:message
// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

//msggen:message
// LoginResponse 登录响应
type LoginResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token"`
	UserID  int64  `json:"user_id"`
}

//msggen:message
// ChatMessage 聊天消息
type ChatMessage struct {
	Channel string `json:"channel"`
	Content string `json:"content"`
	SenderID int64 `json:"sender_id"`
}

// NotAMessage 没有 msggen 标记的结构体，不应被解析
type NotAMessage struct {
	X int
}
