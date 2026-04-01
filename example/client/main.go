package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
)

type LoginRequest struct {
	Type     string `json:"type"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Type    string `json:"type"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func main() {
	conn, err := net.Dial("tcp", "localhost:8888")
	if err != nil {
		fmt.Println("连接失败:", err)
		os.Exit(1)
	}
	defer conn.Close()

	// 发送登录请求
	loginReq := &LoginRequest{
		Type:     "LoginRequest",
		Username: "player1",
		Password: "123456",
	}

	data, _ := json.Marshal(loginReq)

	// 写入消息长度（2字节大端序）
	msgLen := uint16(len(data))
	lenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(lenBuf, msgLen)

	conn.Write(lenBuf)
	conn.Write(data)

	fmt.Println("已发送登录请求")

	// 读取响应
	respLenBuf := make([]byte, 2)
	_, err = conn.Read(respLenBuf)
	if err != nil {
		fmt.Println("读取响应长度失败:", err)
		return
	}

	respLen := binary.BigEndian.Uint16(respLenBuf)
	respData := make([]byte, respLen)
	_, err = conn.Read(respData)
	if err != nil {
		fmt.Println("读取响应数据失败:", err)
		return
	}

	var resp LoginResponse
	json.Unmarshal(respData, &resp)
	fmt.Printf("收到响应: %+v\n", resp)
}
