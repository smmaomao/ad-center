package fatigue

import (
	"testing"

	"adcenter/internal/config"
)

func TestMemoryStore_控制1窗口上限(t *testing.T) {
	// 控制1：20 分钟窗内最多看 3 次，第 4 次拦截。
	s := NewMemory()
	cfg := config.FatigueConfig{Enabled: true, WindowMinutes: 20, WindowMax: 3, DailyMax: 100}
	uid, cid := "u1", "c1"
	for i := 1; i <= 3; i++ {
		if blocked, _ := s.Check(uid, cid, cfg); blocked {
			t.Fatalf("第 %d 次观看不应被拦（上限 3）", i)
		}
		s.Record(uid, cid, cfg)
	}
	if blocked, reason := s.Check(uid, cid, cfg); !blocked || reason != "window_exceeded" {
		t.Fatalf("第 4 次应 window_exceeded，实际 blocked=%v reason=%q", blocked, reason)
	}
}

func TestMemoryStore_控制2每日上限(t *testing.T) {
	// 控制2：每日（滚动24h）最多看 2 次，第 3 次拦截（窗口上限抬高以隔离维度）。
	s := NewMemory()
	cfg := config.FatigueConfig{Enabled: true, WindowMinutes: 20, WindowMax: 100, DailyMax: 2}
	uid, cid := "u1", "c1"
	for i := 1; i <= 2; i++ {
		if blocked, _ := s.Check(uid, cid, cfg); blocked {
			t.Fatalf("第 %d 次观看不应被拦（上限 2）", i)
		}
		s.Record(uid, cid, cfg)
	}
	if blocked, reason := s.Check(uid, cid, cfg); !blocked || reason != "daily_exceeded" {
		t.Fatalf("第 3 次应 daily_exceeded，实际 blocked=%v reason=%q", blocked, reason)
	}
}

func TestMemoryStore_不同用户素材互不干扰(t *testing.T) {
	s := NewMemory()
	cfg := config.FatigueConfig{Enabled: true, WindowMinutes: 20, WindowMax: 1, DailyMax: 1}
	// u1 已看满 cr_a
	s.Record("u1", "cr_a", cfg)
	if blocked, _ := s.Check("u1", "cr_a", cfg); !blocked {
		t.Fatal("u1 看满 cr_a 应被拦")
	}
	// 不同素材 / 不同用户不受影响
	if blocked, _ := s.Check("u1", "cr_b", cfg); blocked {
		t.Fatal("u1 看 cr_b 不应被拦（维度独立）")
	}
	if blocked, _ := s.Check("u2", "cr_a", cfg); blocked {
		t.Fatal("u2 看 cr_a 不应被拦（用户独立）")
	}
}

func TestMemoryStore_计数与配置解耦(t *testing.T) {
	// Record 在 impression 调用，Check 在 decision 调用；这里验证两者用同一份
	// 配置口径。计数器在 Record 时按当时 cfg 设 TTL，Check 时按当时 cfg 判上限。
	s := NewMemory()
	cfg := config.FatigueConfig{Enabled: true, WindowMinutes: 20, WindowMax: 2, DailyMax: 5}
	s.Record("u", "c", cfg)
	if blocked, _ := s.Check("u", "c", cfg); blocked {
		t.Fatal("仅 1 次观看不应被拦")
	}
	// 即便把上限临时调大，已有计数不受影响了（判上限看当前 cfg）
	loose := config.FatigueConfig{Enabled: true, WindowMinutes: 20, WindowMax: 10, DailyMax: 10}
	if blocked, _ := s.Check("u", "c", loose); blocked {
		t.Fatal("计数 1 < 放松后的上限 10，不应被拦")
	}
}
