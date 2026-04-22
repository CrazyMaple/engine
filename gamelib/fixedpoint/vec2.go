package fixedpoint

import "fmt"

// Vec2 定点数二维向量
type Vec2 struct {
	X, Y Fixed
}

// NewVec2 创建向量
func NewVec2(x, y Fixed) Vec2 {
	return Vec2{X: x, Y: y}
}

// Vec2FromInt 从整数创建向量
func Vec2FromInt(x, y int) Vec2 {
	return Vec2{X: FromInt(x), Y: FromInt(y)}
}

// Vec2FromFloat64 从浮点数创建向量
func Vec2FromFloat64(x, y float64) Vec2 {
	return Vec2{X: FromFloat64(x), Y: FromFloat64(y)}
}

// Vec2Zero 零向量
var Vec2Zero = Vec2{}

// String 格式化输出
func (v Vec2) String() string {
	return fmt.Sprintf("(%s, %s)", v.X, v.Y)
}

// Add 向量加法
func (v Vec2) Add(other Vec2) Vec2 {
	return Vec2{X: v.X + other.X, Y: v.Y + other.Y}
}

// Sub 向量减法
func (v Vec2) Sub(other Vec2) Vec2 {
	return Vec2{X: v.X - other.X, Y: v.Y - other.Y}
}

// Scale 标量乘法
func (v Vec2) Scale(s Fixed) Vec2 {
	return Vec2{X: v.X.Mul(s), Y: v.Y.Mul(s)}
}

// Neg 取反
func (v Vec2) Neg() Vec2 {
	return Vec2{X: -v.X, Y: -v.Y}
}

// Dot 点积
func (v Vec2) Dot(other Vec2) Fixed {
	return v.X.Mul(other.X).Add(v.Y.Mul(other.Y))
}

// Cross 叉积（2D 叉积返回标量，等于 z 分量）
func (v Vec2) Cross(other Vec2) Fixed {
	return v.X.Mul(other.Y).Sub(v.Y.Mul(other.X))
}

// LenSq 长度的平方（避免开方）
func (v Vec2) LenSq() Fixed {
	return v.X.Mul(v.X).Add(v.Y.Mul(v.Y))
}

// Len 向量长度
func (v Vec2) Len() Fixed {
	return Sqrt(v.LenSq())
}

// Normalize 归一化（返回单位向量）
func (v Vec2) Normalize() Vec2 {
	length := v.Len()
	if length == 0 {
		return Vec2Zero
	}
	return Vec2{X: v.X.Div(length), Y: v.Y.Div(length)}
}

// DistanceSq 到另一个点的距离平方
func (v Vec2) DistanceSq(other Vec2) Fixed {
	return v.Sub(other).LenSq()
}

// Distance 到另一个点的距离
func (v Vec2) Distance(other Vec2) Fixed {
	return Sqrt(v.DistanceSq(other))
}

// Lerp 线性插值
func (v Vec2) Lerp(target Vec2, t Fixed) Vec2 {
	return Vec2{
		X: Lerp(v.X, target.X, t),
		Y: Lerp(v.Y, target.Y, t),
	}
}

// Rotate 旋转指定角度（弧度）
func (v Vec2) Rotate(angle Fixed) Vec2 {
	s := Sin(angle)
	c := Cos(angle)
	return Vec2{
		X: v.X.Mul(c).Sub(v.Y.Mul(s)),
		Y: v.X.Mul(s).Add(v.Y.Mul(c)),
	}
}

// Angle 返回向量的角度（弧度，相对于 X 正轴）
func (v Vec2) Angle() Fixed {
	return Atan2(v.Y, v.X)
}

// AngleTo 返回到目标向量的夹角
func (v Vec2) AngleTo(other Vec2) Fixed {
	return Atan2(v.Cross(other), v.Dot(other))
}

// Perpendicular 返回垂直向量（逆时针 90 度）
func (v Vec2) Perpendicular() Vec2 {
	return Vec2{X: -v.Y, Y: v.X}
}

// Reflect 反射向量（normal 为法线方向，须为单位向量）
func (v Vec2) Reflect(normal Vec2) Vec2 {
	d := v.Dot(normal).Mul(Two)
	return Vec2{
		X: v.X - normal.X.Mul(d),
		Y: v.Y - normal.Y.Mul(d),
	}
}

// ClampLen 限制向量长度
func (v Vec2) ClampLen(maxLen Fixed) Vec2 {
	lenSq := v.LenSq()
	maxSq := maxLen.Mul(maxLen)
	if lenSq <= maxSq {
		return v
	}
	return v.Normalize().Scale(maxLen)
}

// Equals 判断两个向量是否相等
func (v Vec2) Equals(other Vec2) bool {
	return v.X == other.X && v.Y == other.Y
}
