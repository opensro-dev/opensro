package movement

import "math"

// client 43EC10/43EC80: preserve the authored angle constant and float stores.
func vertexDirection(index byte) (float64, float64) {
	a := contactF32(float64(index) * 0.024543600156903267)
	return contactF32(math.Cos(a)), -contactF32(math.Sin(a))
}

// client 4287A8..4288A6 biases the ORIGINAL source, with ties selecting vertex 1.
// Outside blocking does not admit an object owner.
func outsideEdgeStart(x, z, hx, hz, ax, az, bx, bz float64, ia, ib byte) (float64, float64) {
	f := contactF32
	distance := func(vx, vz float64) float64 { dx, dz := f(vx-hx), f(vz-hz); return f(dx*dx + dz*dz) }
	index := ib
	if distance(ax, az) < distance(bx, bz) {
		index = ia
	}
	nx, nz := vertexDirection(index)
	const scale = 0.009999999776482582
	return f(f(x) + f(nx*scale)), f(f(z) + f(nz*scale))
}
