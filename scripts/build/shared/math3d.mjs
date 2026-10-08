/** Multiply quaternions in [x, y, z, w] order. */
export function quatMultiply( a, b ) {
	const [ax, ay, az, aw] = a;
	const [bx, by, bz, bw] = b;
	return [
		aw * bx + ax * bw + ay * bz - az * by,
		aw * by - ax * bz + ay * bw + az * bx,
		aw * bz + ax * by - ay * bx + az * bw,
		aw * bw - ax * bx - ay * by - az * bz
	];
}

/** Rotate one three-component vector by a quaternion in [x, y, z, w] order. */
export function quatRotateVector( q, vector ) {
	const [vx, vy, vz] = vector;
	return quatRotateComponents( q, vx, vy, vz );
}

/** Component form for tight build-time vertex loops; avoids an input-array allocation. */
export function quatRotateComponents( q, vx, vy, vz ) {
	const [x, y, z, w] = q;
	const ux = y * vz - z * vy;
	const uy = z * vx - x * vz;
	const uz = x * vy - y * vx;
	return [
		vx + 2 * (w * ux + (y * uz - z * uy)),
		vy + 2 * (w * uy + (z * ux - x * uz)),
		vz + 2 * (w * uz + (x * uy - y * ux))
	];
}
