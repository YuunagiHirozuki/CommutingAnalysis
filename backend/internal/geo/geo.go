// Package geo 提供地理计算:Haversine 距离、经纬度网格化、GeoJSON 生成。
package geo

import "math"

const earthRadiusM = 6371000 // 平均地球半径(米)

// Haversine 两点球面距离(米)。纬度/经度顺序,站间距离误差 < 0.5%,满足可达性估算。
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}

// MetersPerDegLat / MetersPerDegLon 指定纬度下每度经纬度对应的米数。
func MetersPerDegLat() float64          { return 111320 }
func MetersPerDegLon(lat float64) float64 { return 111320 * math.Cos(lat*math.Pi/180) }
