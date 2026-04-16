package fixedpoint

import (
	"fmt"
	"math"
)

// Fixed Q16.16 定点数类型
// 高 16 位为整数部分（有符号），低 16 位为小数部分
// 表示范围：[-32768, 32767.999984741]，精度：1/65536 ≈ 0.000015
type Fixed int32

const (
	// shift 小数位数
	shift = 16
	// one 定点数 1.0
	one Fixed = 1 << shift
	// half 定点数 0.5
	half Fixed = 1 << (shift - 1)
	// fracMask 小数部分掩码
	fracMask Fixed = one - 1
	// maxFixed 最大值
	maxFixed Fixed = math.MaxInt32
	// minFixed 最小值
	minFixed Fixed = math.MinInt32
)

// 常用常量
var (
	Zero  Fixed = 0
	One   Fixed = one
	Half  Fixed = half
	Two   Fixed = 2 << shift
	Neg1  Fixed = -one
	Pi    Fixed = 205887  // π ≈ 3.14159 * 65536
	Pi2   Fixed = 411775  // 2π
	PiDiv2 Fixed = 102944 // π/2
	E     Fixed = 178145  // e ≈ 2.71828 * 65536
)

// FromInt 整数转定点数
func FromInt(v int) Fixed {
	return Fixed(v) << shift
}

// FromFloat64 浮点数转定点数
func FromFloat64(v float64) Fixed {
	return Fixed(v * float64(one))
}

// ToInt 转整数（截断小数，向零取整）
func (f Fixed) ToInt() int {
	return int(int32(f) >> shift)
}

// ToFloat64 转浮点数（调试用）
func (f Fixed) ToFloat64() float64 {
	return float64(f) / float64(one)
}

// Raw 返回底层原始 int32 值
func (f Fixed) Raw() int32 {
	return int32(f)
}

// FromRaw 从原始 int32 创建定点数
func FromRaw(v int32) Fixed {
	return Fixed(v)
}

// String 格式化输出
func (f Fixed) String() string {
	return fmt.Sprintf("%.6f", f.ToFloat64())
}

// Add 加法
func (f Fixed) Add(other Fixed) Fixed {
	return f + other
}

// Sub 减法
func (f Fixed) Sub(other Fixed) Fixed {
	return f - other
}

// Mul 乘法（使用 int64 中间结果防溢出）
func (f Fixed) Mul(other Fixed) Fixed {
	return Fixed(int64(f) * int64(other) >> shift)
}

// Div 除法（使用 int64 中间结果保持精度）
func (f Fixed) Div(other Fixed) Fixed {
	if other == 0 {
		if f >= 0 {
			return maxFixed
		}
		return minFixed
	}
	return Fixed(int64(f) << shift / int64(other))
}

// Neg 取反
func (f Fixed) Neg() Fixed {
	return -f
}

// Abs 绝对值
func (f Fixed) Abs() Fixed {
	if f < 0 {
		return -f
	}
	return f
}

// Floor 向下取整（向负无穷方向）
func (f Fixed) Floor() Fixed {
	return f & ^fracMask
}

// Ceil 向上取整（向正无穷方向）
func (f Fixed) Ceil() Fixed {
	return (f + fracMask) & ^fracMask
}

// Round 四舍五入
func (f Fixed) Round() Fixed {
	return (f + half) & ^fracMask
}

// Frac 小数部分（始终非负）
func (f Fixed) Frac() Fixed {
	return f - f.Floor()
}

// Min 取较小值
func Min(a, b Fixed) Fixed {
	if a < b {
		return a
	}
	return b
}

// Max 取较大值
func Max(a, b Fixed) Fixed {
	if a > b {
		return a
	}
	return b
}

// Clamp 值域限制
func Clamp(v, lo, hi Fixed) Fixed {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Lerp 线性插值：a + t*(b-a)
func Lerp(a, b, t Fixed) Fixed {
	return a.Add(b.Sub(a).Mul(t))
}

// Sign 符号函数：正数返回 1，负数返回 -1，零返回 0
func (f Fixed) Sign() Fixed {
	if f > 0 {
		return One
	}
	if f < 0 {
		return Neg1
	}
	return Zero
}

// Cmp 比较：返回 -1, 0, 1
func (f Fixed) Cmp(other Fixed) int {
	if f < other {
		return -1
	}
	if f > other {
		return 1
	}
	return 0
}
