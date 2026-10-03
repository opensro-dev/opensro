/*
===========================================================================

strict-gpu.mjs - a WebGPU device fake that validates a submit as a browser does

Every bind group, render bundle, pass and command buffer remembers the
buffers and textures it names. queue.submit throws when one of them was
destroyed: the validation Chrome reports as "used in submit while
destroyed", which a fake that only counts calls cannot catch.

===========================================================================
*/

// The WebGPU usage constants (the specification's values).
export const GPU_BUFFER_USAGE = {
	MAP_READ: 1,
	MAP_WRITE: 2,
	COPY_SRC: 4,
	COPY_DST: 8,
	INDEX: 16,
	VERTEX: 32,
	UNIFORM: 64,
	STORAGE: 128,
	INDIRECT: 256,
	QUERY_RESOLVE: 512
};
export const GPU_TEXTURE_USAGE = {
	COPY_SRC: 1,
	COPY_DST: 2,
	TEXTURE_BINDING: 4,
	STORAGE_BINDING: 8,
	RENDER_ATTACHMENT: 16
};
export const GPU_SHADER_STAGE = { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 };

/*
================
createStrictGpu

device is the fake GPUDevice. log lists, in order, every "destroy <label>"
and "submit <label>"; live counts the buffers and textures not destroyed.
================
*/
export function createStrictGpu() {
	const log = [], resources = [];
	const named = resource => `[${resource.kind} ${resource.label ? `"${resource.label}"` : "(unlabeled)"}]`;
	/*
	================
	resource

	A buffer, texture or query set: destroyed once, and named in the log.
	================
	*/
	function resource( kind, descriptor = {} ) {
		const created = {
			kind,
			label: descriptor.label ?? "",
			size: descriptor.size,
			destroyed: false,
			destroy() {
				if ( this.destroyed ) return;
				this.destroyed = true;
				log.push( "destroy " + (this.label || kind.toLowerCase()) );
			}
		};
		resources.push( created );
		return created;
	}
	/*
	================
	recorder

	The commands a pass or bundle encoder accepts, gathering what they name
	into used.
	================
	*/
	function recorder( used ) {
		return {
			setPipeline() {},
			setBlendConstant() {},
			setViewport() {},
			setScissorRect() {},
			setBindGroup( _index, group ) {
				for ( const item of group?.used ?? [] ) used.add( item );
			},
			setVertexBuffer( _slot, buffer ) {
				used.add( buffer );
			},
			setIndexBuffer( buffer ) {
				used.add( buffer );
			},
			executeBundles( bundles ) {
				for ( const bundle of bundles ) for ( const item of bundle.used ) used.add( item );
			},
			draw() {},
			drawIndexed() {},
			dispatchWorkgroups() {},
			beginOcclusionQuery() {},
			endOcclusionQuery() {},
			end() {}
		};
	}
	const pipeline = () => ({
		getBindGroupLayout() {
			return {};
		}
	});
	const device = {
		features: new Set(),
		limits: {},
		lost: new Promise( () => {} ),
		addEventListener() {},
		destroy() {},
		pushErrorScope() {},
		popErrorScope: async () => null,
		createShaderModule() {
			return { getCompilationInfo: async () => ({ messages: [] }) };
		},
		createBindGroupLayout() {
			return {};
		},
		createPipelineLayout() {
			return {};
		},
		createSampler() {
			return {};
		},
		createRenderPipeline: pipeline,
		createRenderPipelineAsync: async () => pipeline(),
		createComputePipeline: pipeline,
		createComputePipelineAsync: async () => pipeline(),
		createBuffer( descriptor ) {
			return resource( "Buffer", descriptor );
		},
		createQuerySet( descriptor ) {
			return resource( "QuerySet", descriptor );
		},
		createTexture( descriptor ) {
			const texture = resource( "Texture", descriptor );
			texture.createView = () => ({ texture });
			return texture;
		},
		/*
		================
		createBindGroup

		A binding names a buffer ({ buffer }), a texture (its view) or a sampler.
		================
		*/
		createBindGroup( { entries } ) {
			const used = [];
			for ( const entry of entries ) {
				const bound = entry.resource?.buffer ?? entry.resource?.texture;
				if ( bound ) used.push( bound );
			}
			return { used };
		},
		createRenderBundleEncoder() {
			const used = new Set();
			return { ...recorder( used ), finish: () => ({ used }) };
		},
		createCommandEncoder( descriptor = {} ) {
			const used = new Set();
			/*
			================
			attachments

			A render pass names the texture behind each of its views.
			================
			*/
			function attachments( pass ) {
				for ( const attachment of [ ...(pass.colorAttachments ?? []), pass.depthStencilAttachment ] ) {
					if ( attachment?.view?.texture ) used.add( attachment.view.texture );
				}
				if ( pass.occlusionQuerySet ) used.add( pass.occlusionQuerySet );
			}
			return {
				beginRenderPass( pass ) {
					attachments( pass );
					return recorder( used );
				},
				beginComputePass() {
					return recorder( used );
				},
				copyBufferToBuffer( source, _sourceOffset, target ) {
					used.add( source ).add( target );
				},
				copyTextureToTexture( source, target ) {
					used.add( source.texture ).add( target.texture );
				},
				resolveQuerySet( querySet, _first, _count, target ) {
					used.add( querySet ).add( target );
				},
				finish: () => ({ label: descriptor.label ?? "", used })
			};
		},
		queue: {
			writeBuffer( buffer ) {
				if ( buffer.destroyed ) throw Error( `${named( buffer )} written while destroyed.` );
			},
			writeTexture() {},
			copyExternalImageToTexture() {},
			/*
			================
			submit

			The rule under test: a command buffer may only name live resources.
			================
			*/
			submit( buffers ) {
				for ( const buffer of buffers ) {
					for ( const item of buffer.used ) {
						if ( item?.destroyed ) throw Error( `${named( item )} used in submit while destroyed.` );
					}
					log.push( "submit " + buffer.label );
				}
			}
		}
	};
	return { device, log, live: () => resources.filter( created => !created.destroyed ).length };
}
