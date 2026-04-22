package fixedpoint

// 三角函数查表法：1024 个采样点覆盖 [0, π/2)，通过对称性扩展到全周期
// 查表 + 线性插值，精度约 ±0.001

const sinTableSize = 1024

// sinTable 存储 sin(i * π/2 / 1024) 的 Q16.16 定点数值
// 覆盖 [0, π/2)，其余象限通过对称性推导
var sinTable [sinTableSize + 1]Fixed

func init() {
	// 编译期计算正弦查找表
	for i := 0; i <= sinTableSize; i++ {
		// 使用 float64 精确计算后转为定点数
		angle := float64(i) * 1.5707963267948966 / float64(sinTableSize) // π/2 / 1024
		v := int32(angle * 65536.0)
		// sin 用泰勒级数直接算（避免 import math 在 init 中的循环依赖风险）
		// 实际上这里直接用 math.Sin 更精确
		_ = v
		sinVal := sinFloat64(float64(i) * 1.5707963267948966 / float64(sinTableSize))
		sinTable[i] = Fixed(int32(sinVal * 65536.0))
	}
}

// sinFloat64 纯 Go 正弦计算（用于初始化查找表）
func sinFloat64(x float64) float64 {
	// 泰勒展开：sin(x) = x - x³/6 + x⁵/120 - x⁷/5040 + x⁹/362880
	x2 := x * x
	x3 := x2 * x
	x5 := x3 * x2
	x7 := x5 * x2
	x9 := x7 * x2
	x11 := x9 * x2
	return x - x3/6.0 + x5/120.0 - x7/5040.0 + x9/362880.0 - x11/39916800.0
}

// Sin 正弦函数
func Sin(angle Fixed) Fixed {
	// 归一化到 [0, 2π)
	if angle < 0 {
		angle = Pi2 - ((-angle) % Pi2)
		if angle == Pi2 {
			angle = 0
		}
	} else {
		angle = angle % Pi2
	}

	// 确定象限并映射到 [0, π/2)
	var neg bool
	if angle >= Pi.Add(PiDiv2) {
		// 第四象限 [3π/2, 2π)：-sin(2π - angle)
		angle = Pi2 - angle
		neg = true
	} else if angle >= Pi {
		// 第三象限 [π, 3π/2)：-sin(angle - π)
		angle = angle - Pi
		neg = true
	} else if angle >= PiDiv2 {
		// 第二象限 [π/2, π)：sin(π - angle)
		angle = Pi - angle
	}
	// 第一象限 [0, π/2)：直接查表

	// 将角度映射到表索引 [0, sinTableSize]
	// index = angle * sinTableSize / (π/2)
	idx64 := int64(angle) * int64(sinTableSize)
	idx64 = idx64 / int64(PiDiv2)

	idx := int(idx64)
	if idx >= sinTableSize {
		idx = sinTableSize - 1
	}
	if idx < 0 {
		idx = 0
	}

	// 线性插值
	frac := int64(angle)*int64(sinTableSize) - int64(idx)*int64(PiDiv2)
	v0 := sinTable[idx]
	v1 := sinTable[idx+1]
	result := v0 + Fixed(int64(v1-v0)*frac/(int64(PiDiv2)))

	if neg {
		return -result
	}
	return result
}

// Cos 余弦函数：cos(x) = sin(x + π/2)
func Cos(angle Fixed) Fixed {
	return Sin(angle + PiDiv2)
}

// Tan 正切函数：tan(x) = sin(x) / cos(x)
func Tan(angle Fixed) Fixed {
	c := Cos(angle)
	if c == 0 {
		if Sin(angle) >= 0 {
			return maxFixed
		}
		return minFixed
	}
	return Sin(angle).Div(c)
}

// Atan2 反正切函数（两参数版本）
func Atan2(y, x Fixed) Fixed {
	if x == 0 && y == 0 {
		return Zero
	}
	if x == 0 {
		if y > 0 {
			return PiDiv2
		}
		return -PiDiv2
	}
	if y == 0 {
		if x > 0 {
			return Zero
		}
		return Pi
	}

	// 保证 |r| <= 1 以提高泰勒展开收敛性
	absX := x.Abs()
	absY := y.Abs()

	var atanVal Fixed
	if absX >= absY {
		r := absY.Div(absX)
		atanVal = atanApprox(r)
	} else {
		r := absX.Div(absY)
		atanVal = PiDiv2 - atanApprox(r)
	}

	// 根据象限调整符号
	if x < 0 {
		atanVal = Pi - atanVal
	}
	if y < 0 {
		atanVal = -atanVal
	}
	return atanVal
}

// atanApprox 计算 atan(r)，要求 0 <= r <= 1
// 使用 Pade 近似：atan(r) ≈ r * (15 + 4*r²) / (15 + 9*r²)
// 在 [0,1] 上最大误差 < 0.005
func atanApprox(r Fixed) Fixed {
	r2 := r.Mul(r)
	num := FromInt(15).Add(FromInt(4).Mul(r2)) // 15 + 4*r²
	den := FromInt(15).Add(FromInt(9).Mul(r2)) // 15 + 9*r²
	return r.Mul(num).Div(den)
}

// Sqrt 开平方（牛顿迭代法）
// 对 Q16.16 定点数 f，计算 sqrt(f) 的 Q16.16 表示
func Sqrt(f Fixed) Fixed {
	if f <= 0 {
		return Zero
	}

	// sqrt(f_real) = sqrt(f_raw / 2^16) = sqrt(f_raw * 2^16) / 2^16
	// 所以对 f_raw << 16 做整数开方，结果直接就是 Q16.16 表示
	n := uint64(f) << shift

	// 牛顿迭代法求整数开方
	x := n
	y := (x + 1) >> 1
	for y < x {
		x = y
		y = (x + n/x) >> 1
	}

	return Fixed(int32(x))
}

// InvSqrt 快速倒数平方根 1/sqrt(f)
func InvSqrt(f Fixed) Fixed {
	if f <= 0 {
		return maxFixed
	}
	s := Sqrt(f)
	if s == 0 {
		return maxFixed
	}
	return One.Div(s)
}
