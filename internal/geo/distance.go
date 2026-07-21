package geo

import "math"

// DistanceMeters 两点球面距离（米），Haversine。
func DistanceMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusM = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	φ1 := toRad(lat1)
	φ2 := toRad(lat2)
	Δφ := toRad(lat2 - lat1)
	Δλ := toRad(lng2 - lng1)
	a := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*math.Sin(Δλ/2)*math.Sin(Δλ/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}
