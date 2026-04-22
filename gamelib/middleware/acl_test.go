package middleware

import (
	"testing"
)

func TestACLDefaultAllow(t *testing.T) {
	acl := NewACL()
	if acl.Check("any-sender", "any-type") != PermAllow {
		t.Fatal("default should be allow")
	}
}

func TestACLDenyAll(t *testing.T) {
	acl := NewACL()
	acl.AddRule(ACLRule{Permission: PermDeny})
	if acl.Check("sender", "type") != PermDeny {
		t.Fatal("should deny all")
	}
}

func TestACLDenySpecificSender(t *testing.T) {
	acl := NewACL()
	acl.AddRule(ACLRule{SenderPattern: "bad-actor", Permission: PermDeny})

	if acl.Check("bad-actor", "any") != PermDeny {
		t.Fatal("should deny bad-actor")
	}
	if acl.Check("good-actor", "any") != PermAllow {
		t.Fatal("should allow good-actor")
	}
}

func TestACLDenySpecificMessageType(t *testing.T) {
	acl := NewACL()
	acl.AddRule(ACLRule{MessageType: "*DangerousMsg", Permission: PermDeny})

	if acl.Check("any", "*DangerousMsg") != PermDeny {
		t.Fatal("should deny DangerousMsg")
	}
	if acl.Check("any", "*SafeMsg") != PermAllow {
		t.Fatal("should allow SafeMsg")
	}
}

func TestACLGlobPattern(t *testing.T) {
	acl := NewACL()
	acl.AddRule(ACLRule{SenderPattern: "external/*", Permission: PermDeny})

	if acl.Check("external/client1", "any") != PermDeny {
		t.Fatal("should deny external/client1")
	}
	if acl.Check("internal/service1", "any") != PermAllow {
		t.Fatal("should allow internal/service1")
	}
}

func TestACLLastRuleWins(t *testing.T) {
	acl := NewACL()
	acl.AddRule(ACLRule{Permission: PermDeny})                           // 拒绝所有
	acl.AddRule(ACLRule{SenderPattern: "admin", Permission: PermAllow}) // 允许 admin

	if acl.Check("admin", "any") != PermAllow {
		t.Fatal("admin should be allowed (last matching rule wins)")
	}
	if acl.Check("user", "any") != PermDeny {
		t.Fatal("user should be denied")
	}
}

func TestACLRemoveRule(t *testing.T) {
	acl := NewACL()
	acl.AddRule(ACLRule{Permission: PermDeny})
	acl.RemoveRule(0)

	if acl.Check("any", "any") != PermAllow {
		t.Fatal("should allow after removing deny rule")
	}
}

func TestACLClearRules(t *testing.T) {
	acl := NewACL()
	acl.AddRule(ACLRule{Permission: PermDeny})
	acl.ClearRules()

	if acl.Check("any", "any") != PermAllow {
		t.Fatal("should allow after clearing rules")
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		value    string
		expected bool
	}{
		{"", "anything", true},
		{"exact", "exact", true},
		{"exact", "other", false},
		{"*.go", "main.go", true},
		{"*.go", "main.py", false},
		{"test*", "testing", true},
	}

	for _, tt := range tests {
		got := matchPattern(tt.pattern, tt.value)
		if got != tt.expected {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.expected)
		}
	}
}
