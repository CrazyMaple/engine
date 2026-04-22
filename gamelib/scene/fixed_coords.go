package scene

import "gamelib/fixedpoint"

// FixedPoint2D 定点数坐标（用于帧同步场景的确定性计算）
type FixedPoint2D struct {
	X fixedpoint.Fixed
	Y fixedpoint.Fixed
}

// NewFixedPoint2D 创建定点数坐标
func NewFixedPoint2D(x, y fixedpoint.Fixed) FixedPoint2D {
	return FixedPoint2D{X: x, Y: y}
}

// FixedPoint2DFromFloat 从 float32 创建定点数坐标
func FixedPoint2DFromFloat(x, y float32) FixedPoint2D {
	return FixedPoint2D{
		X: fixedpoint.FromFloat64(float64(x)),
		Y: fixedpoint.FromFloat64(float64(y)),
	}
}

// ToFloat32 转换为 float32 坐标（用于渲染/AOI 查询）
func (fp FixedPoint2D) ToFloat32() (float32, float32) {
	return float32(fp.X.ToFloat64()), float32(fp.Y.ToFloat64())
}

// ToVec2 转为定点数向量
func (fp FixedPoint2D) ToVec2() fixedpoint.Vec2 {
	return fixedpoint.NewVec2(fp.X, fp.Y)
}

// DistanceSq 计算到另一点的距离平方（定点数精度，避免开方）
func (fp FixedPoint2D) DistanceSq(other FixedPoint2D) fixedpoint.Fixed {
	dx := fp.X.Sub(other.X)
	dy := fp.Y.Sub(other.Y)
	return dx.Mul(dx).Add(dy.Mul(dy))
}

// Distance 计算到另一点的距离（定点数精度）
func (fp FixedPoint2D) Distance(other FixedPoint2D) fixedpoint.Fixed {
	return fixedpoint.Sqrt(fp.DistanceSq(other))
}

// InRange 判断是否在指定范围内（使用距离平方避免开方）
func (fp FixedPoint2D) InRange(other FixedPoint2D, rangeVal fixedpoint.Fixed) bool {
	return fp.DistanceSq(other) <= rangeVal.Mul(rangeVal)
}

// Add 坐标加法
func (fp FixedPoint2D) Add(other FixedPoint2D) FixedPoint2D {
	return FixedPoint2D{X: fp.X.Add(other.X), Y: fp.Y.Add(other.Y)}
}

// Sub 坐标减法
func (fp FixedPoint2D) Sub(other FixedPoint2D) FixedPoint2D {
	return FixedPoint2D{X: fp.X.Sub(other.X), Y: fp.Y.Sub(other.Y)}
}

// Lerp 坐标线性插值
func (fp FixedPoint2D) Lerp(target FixedPoint2D, t fixedpoint.Fixed) FixedPoint2D {
	return FixedPoint2D{
		X: fixedpoint.Lerp(fp.X, target.X, t),
		Y: fixedpoint.Lerp(fp.Y, target.Y, t),
	}
}

// EntityToFixed 将 GridEntity 的 float32 坐标转为定点数坐标
func EntityToFixed(e *GridEntity) FixedPoint2D {
	return FixedPoint2DFromFloat(e.X, e.Y)
}
