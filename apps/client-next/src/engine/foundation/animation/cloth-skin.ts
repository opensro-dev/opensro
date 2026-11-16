/*
===========================================================================

cloth-skin.ts - one cloth vertex skinned on the CPU, as AEADE0 anchors it

The cloth solver needs its pinned anchors in model space on the CPU. Both
cloth owners (cloth-vertices.ts, which writes a whole vertex stream, and
cloth-stream.ts, which leaves the pins to the GPU between solver steps)
skin a vertex with this one function, so they agree to the bit.

===========================================================================
*/
import type { Geometry } from "@/engine/contracts/geometry";

/*
================
skinClothVertex

Vertex i of mesh under palette (16 floats per joint), written to out as
[x, y, z, nx, ny, nz]. An unskinned mesh yields its own position and
normal; a missing normal is +Y. Each axis keeps its double-precision
accumulation order; only the influence lookup is shared.
================
*/
export function skinClothVertex( mesh: Geometry, palette: Float32Array, i: number, out: Float64Array ) {
	const at = i * 3;
	const x = mesh.positions[at]!, y = mesh.positions[at + 1]!, z = mesh.positions[at + 2]!;
	const nx = mesh.normals?.[at] ?? 0, ny = mesh.normals?.[at + 1] ?? 1, nz = mesh.normals?.[at + 2] ?? 0;
	let px = x, py = y, pz = z, normalX = nx, normalY = ny, normalZ = nz;
	if ( mesh.joints && mesh.weights ) {
		px =
			py =
			pz =
			normalX =
			normalY =
			normalZ =
				0;
		for ( let joint = 0; joint < 4; joint++ ) {
			const weight = mesh.weights[i * 4 + joint]!;
			if ( !weight ) continue;
			const base = mesh.joints[i * 4 + joint]! * 16;
			let ax = palette[base + 12]!, ay = palette[base + 13]!, az = palette[base + 14]!;
			let bx = 0, by = 0, bz = 0;
			ax += palette[base]! * x;
			ay += palette[base + 1]! * x;
			az += palette[base + 2]! * x;
			bx += palette[base]! * nx;
			by += palette[base + 1]! * nx;
			bz += palette[base + 2]! * nx;
			ax += palette[base + 4]! * y;
			ay += palette[base + 5]! * y;
			az += palette[base + 6]! * y;
			bx += palette[base + 4]! * ny;
			by += palette[base + 5]! * ny;
			bz += palette[base + 6]! * ny;
			ax += palette[base + 8]! * z;
			ay += palette[base + 9]! * z;
			az += palette[base + 10]! * z;
			bx += palette[base + 8]! * nz;
			by += palette[base + 9]! * nz;
			bz += palette[base + 10]! * nz;
			px += ax * weight;
			py += ay * weight;
			pz += az * weight;
			normalX += bx * weight;
			normalY += by * weight;
			normalZ += bz * weight;
		}
	}
	out[0] = px;
	out[1] = py;
	out[2] = pz;
	out[3] = normalX;
	out[4] = normalY;
	out[5] = normalZ;
}
