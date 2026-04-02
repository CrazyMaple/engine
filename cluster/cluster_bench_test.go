package cluster

import (
	"fmt"
	"testing"
)

func BenchmarkConsistentHashGetMember(b *testing.B) {
	for _, memberCount := range []int{3, 10, 50} {
		b.Run(fmt.Sprintf("members_%d", memberCount), func(b *testing.B) {
			ch := NewConsistentHash()
			members := make([]*Member, memberCount)
			for i := 0; i < memberCount; i++ {
				members[i] = &Member{
					Address: fmt.Sprintf("127.0.0.1:%d", 8000+i),
					Id:      fmt.Sprintf("node-%d", i),
					Kinds:   []string{"game", "chat"},
					Status:  MemberAlive,
				}
			}
			ch.UpdateMembers(members)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ch.GetMember(fmt.Sprintf("player-%d", i%1000), "game")
			}
		})
	}
}

func BenchmarkConsistentHashGetMemberSingleKind(b *testing.B) {
	ch := NewConsistentHash()
	members := make([]*Member, 10)
	for i := 0; i < 10; i++ {
		kinds := []string{"game"}
		if i%2 == 0 {
			kinds = append(kinds, "chat")
		}
		members[i] = &Member{
			Address: fmt.Sprintf("127.0.0.1:%d", 8000+i),
			Id:      fmt.Sprintf("node-%d", i),
			Kinds:   kinds,
			Status:  MemberAlive,
		}
	}
	ch.UpdateMembers(members)

	b.Run("game", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ch.GetMember(fmt.Sprintf("player-%d", i%1000), "game")
		}
	})
	b.Run("chat", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ch.GetMember(fmt.Sprintf("player-%d", i%1000), "chat")
		}
	})
}

func BenchmarkHashCombine(b *testing.B) {
	for i := 0; i < b.N; i++ {
		hashCombine("player-12345", "127.0.0.1:8080")
	}
}
