/*
===========================================================================
NativeLensResources.cs - D3DX lens resource generation without a C++ SDK

Run in 32-bit Windows PowerShell with d3dx9_39.dll to preserve the compressed
mip bytes produced by tools/native-lens-resources.cpp. The caller owns the
generated-asset lock. All Direct3D objects have explicit, local lifetimes.
===========================================================================
*/
using System;
using System.IO;
using System.Runtime.InteropServices;

/*
================
NativeLensResources
================
*/
public static class NativeLensResources {
	const uint D3D_SDK_VERSION = 32;
	const uint D3DDEVTYPE_HAL = 1;
	const uint D3DSWAPEFFECT_DISCARD = 1;
	const uint D3DCREATE_SOFTWARE_VERTEXPROCESSING = 0x20;
	const uint D3DCREATE_FPU_PRESERVE = 0x02;
	const uint D3DLOCK_READONLY = 0x10;
	const uint D3DFMT_A8R8G8B8 = 21;
	const uint D3DFMT_DXT3 = 0x33545844;
	const uint NTX_MAGIC = 0x3158544e;
	const int DDJ_HEADER_SIZE = 20;
	const int MAX_DDJ_BYTES = 16 * 1024 * 1024;
	const int LENS_COUNT = 8;
	const uint WS_POPUP = 0x80000000;

	/*
	================
	PresentParameters - D3DPRESENT_PARAMETERS
	================
	*/
	[StructLayout(LayoutKind.Sequential)]
	struct PresentParameters {
		public uint Width, Height, Format, Count, Multisample, Quality, SwapEffect;
		public IntPtr Window;
		public int Windowed, AutoDepth;
		public uint DepthFormat, Flags, Refresh, Interval;
	}

	/*
	================
	SurfaceDesc - D3DSURFACE_DESC
	================
	*/
	[StructLayout(LayoutKind.Sequential)]
	struct SurfaceDesc {
		public uint Format, Type, Usage, Pool, Multisample, Quality, Width, Height;
	}

	/*
	================
	LockedRect - D3DLOCKED_RECT
	================
	*/
	[StructLayout(LayoutKind.Sequential)]
	struct LockedRect {
		public int Pitch;
		public IntPtr Bits;
	}

	// Windows SDK COM vtable signatures; WINAPI is stdcall in this 32-bit process.
	[UnmanagedFunctionPointer(CallingConvention.StdCall)]
	delegate uint ReleaseMethod(IntPtr self);
	[UnmanagedFunctionPointer(CallingConvention.StdCall)]
	delegate int CreateDeviceMethod(IntPtr self, uint adapter, uint type, IntPtr window,
		uint flags, ref PresentParameters parameters, out IntPtr device);
	[UnmanagedFunctionPointer(CallingConvention.StdCall)]
	delegate uint GetLevelCountMethod(IntPtr self);
	[UnmanagedFunctionPointer(CallingConvention.StdCall)]
	delegate int GetLevelDescMethod(IntPtr self, uint level, out SurfaceDesc desc);
	[UnmanagedFunctionPointer(CallingConvention.StdCall)]
	delegate int LockRectMethod(IntPtr self, uint level, out LockedRect rect, IntPtr area, uint flags);
	[UnmanagedFunctionPointer(CallingConvention.StdCall)]
	delegate int UnlockRectMethod(IntPtr self, uint level);

	[DllImport("d3d9.dll", ExactSpelling = true)]
	static extern IntPtr Direct3DCreate9(uint version);
	[DllImport("d3dx9_39.dll", ExactSpelling = true)]
	static extern int D3DXCreateTextureFromFileInMemory(IntPtr device, byte[] data, uint size, out IntPtr texture);
	[DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
	static extern IntPtr CreateWindowExW(uint extended, string className, string title, uint style,
		int x, int y, int width, int height, IntPtr parent, IntPtr menu, IntPtr instance, IntPtr parameter);
	[DllImport("user32.dll")]
	static extern bool DestroyWindow(IntPtr window);

	/*
	================
	Method - bind a COM slot without owning a second reference
	================
	*/
	static T Method<T>(IntPtr instance, int slot) where T : class {
		IntPtr address = Marshal.ReadIntPtr(Marshal.ReadIntPtr(instance), slot * IntPtr.Size);
		return Marshal.GetDelegateForFunctionPointer(address, typeof(T)) as T;
	}

	/*
	================
	Check
	================
	*/
	static void Check(int result, string operation) {
		if ( result < 0 ) {
			throw new InvalidOperationException(operation + " failed: HRESULT 0x" + result.ToString("x8"));
		}
	}

	/*
	================
	Release
	================
	*/
	static void Release(IntPtr instance) {
		if ( instance != IntPtr.Zero ) {
			Method<ReleaseMethod>(instance, 2)(instance);
		}
	}

	/*
	================
	ReadDds - validate the extracted wrapper before calling native code
	================
	*/
	static byte[] ReadDds(string source) {
		byte[] data = File.ReadAllBytes(source);
		if ( data.Length < 148 || data.Length > MAX_DDJ_BYTES ||
			System.Text.Encoding.ASCII.GetString(data, 0, 12) != "JMXVDDJ 1000" ||
			System.Text.Encoding.ASCII.GetString(data, DDJ_HEADER_SIZE, 4) != "DDS " ) {
			throw new InvalidDataException("Invalid DDJ/DDS input: " + source);
		}
		byte[] dds = new byte[data.Length - DDJ_HEADER_SIZE];
		Buffer.BlockCopy(data, DDJ_HEADER_SIZE, dds, 0, dds.Length);
		return dds;
	}

	/*
	================
	SerializeTexture - retain compressed blocks and strip row pitch padding
	================
	*/
	static byte[] SerializeTexture(IntPtr texture) {
		var getDesc = Method<GetLevelDescMethod>(texture, 17);
		var lockRect = Method<LockRectMethod>(texture, 19);
		var unlockRect = Method<UnlockRectMethod>(texture, 20);
		SurfaceDesc desc;
		Check(getDesc(texture, 0, out desc), "GetLevelDesc");
		if ( desc.Format != D3DFMT_A8R8G8B8 && desc.Format != D3DFMT_DXT3 ) {
			throw new InvalidDataException("Unsupported lens format: " + desc.Format);
		}
		uint levels = Method<GetLevelCountMethod>(texture, 13)(texture);
		using ( var stream = new MemoryStream() ) {
			using ( var writer = new BinaryWriter(stream) ) {
				writer.Write(NTX_MAGIC);
				writer.Write(desc.Width);
				writer.Write(desc.Height);
				writer.Write(desc.Format);
				writer.Write(levels);
				for ( uint level = 0; level < levels; level++ ) {
					Check(getDesc(texture, level, out desc), "GetLevelDesc");
					LockedRect rect;
					Check(lockRect(texture, level, out rect, IntPtr.Zero, D3DLOCK_READONLY), "LockRect");
					try {
						bool compressed = desc.Format == D3DFMT_DXT3;
						uint rows = compressed ? (desc.Height + 3) / 4 : desc.Height;
						int stride = checked((int)(compressed ? ((desc.Width + 3) / 4) * 16 : desc.Width * 4));
						if ( rect.Pitch < stride || rect.Bits == IntPtr.Zero ) {
							throw new InvalidDataException("Invalid lens texture row pitch or pointer");
						}
						byte[] row = new byte[stride];
						for ( int y = 0; y < rows; y++ ) {
							Marshal.Copy(IntPtr.Add(rect.Bits, y * rect.Pitch), row, 0, stride);
							writer.Write(row);
						}
					} finally {
						Check(unlockRect(texture, level), "UnlockRect");
					}
				}
				return stream.ToArray();
			}
		}
	}

	/*
	================
	Build - create one hidden device and publish all eight mip resources
	================
	*/
	public static void Build(string sourceRoot, string outputRoot) {
		if ( IntPtr.Size != 4 ) {
			throw new InvalidOperationException("Lens conversion must run in 32-bit Windows PowerShell.");
		}
		Directory.CreateDirectory(outputRoot);
		IntPtr window = CreateWindowExW(0, "STATIC", "Lens resource build", WS_POPUP,
			0, 0, 64, 64, IntPtr.Zero, IntPtr.Zero, IntPtr.Zero, IntPtr.Zero);
		if ( window == IntPtr.Zero ) {
			throw new System.ComponentModel.Win32Exception(Marshal.GetLastWin32Error());
		}
		IntPtr api = IntPtr.Zero;
		IntPtr device = IntPtr.Zero;
		try {
			api = Direct3DCreate9(D3D_SDK_VERSION);
			if ( api == IntPtr.Zero ) {
				throw new InvalidOperationException("Direct3DCreate9 failed; check the graphics driver.");
			}
			var parameters = new PresentParameters();
			parameters.Window = window;
			parameters.Windowed = 1;
			parameters.SwapEffect = D3DSWAPEFFECT_DISCARD;
			Check(Method<CreateDeviceMethod>(api, 16)(api, 0, D3DDEVTYPE_HAL, window,
				D3DCREATE_SOFTWARE_VERTEXPROCESSING | D3DCREATE_FPU_PRESERVE,
				ref parameters, out device), "CreateDevice");
			for ( int index = 1; index <= LENS_COUNT; index++ ) {
				string name = "lens" + index;
				byte[] data = ReadDds(Path.Combine(sourceRoot, name + ".ddj"));
				IntPtr texture = IntPtr.Zero;
				try {
					Check(D3DXCreateTextureFromFileInMemory(device, data, (uint)data.Length, out texture), name);
					File.WriteAllBytes(Path.Combine(outputRoot, name + ".texture"), SerializeTexture(texture));
				} finally {
					Release(texture);
				}
			}
		} finally {
			Release(device);
			Release(api);
			DestroyWindow(window);
		}
	}
}
