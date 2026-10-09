/*
===========================================================================

assets.mjs - registered asset publication and native-resource contracts

The asset task runs this explicit suite after the generated tree is ready.
Native texture preservation and lens generation share the same compiler and
must both remain in the suite when that compiler changes.

===========================================================================
*/
export default {
	name: "assets",
	description: "Asset build, packing, cache, and authored-resource contracts",
	runner: "node",
	files: [
		"scripts/test/assets/meshCloth.test.mjs",
		"scripts/test/assets/bsrParticleModifiers.test.mjs",
		"scripts/test/assets/skillEffectBinding.test.mjs",
		"scripts/test/assets/assetDeliveryOwnership.test.mjs",
		"scripts/test/assets/nativeUiTexture.test.mjs",
		"scripts/test/assets/nativeLensResources.test.mjs",
		"scripts/test/assets/skyImagePublication.test.mjs",
		"scripts/test/assets/nativeCharacterTextures.test.mjs",
		"scripts/test/assets/fontAtlasPublication.test.mjs",
		"scripts/test/assets/buildSharedUtilities.test.mjs",
		"scripts/test/assets/publicWrite.test.mjs",
		"scripts/test/assets/generatedArtifactArchive.test.mjs",
		"scripts/test/assets/jsonAssetCompression.test.mjs",
		"scripts/test/assets/vatPipeline.test.mjs",
		"scripts/test/assets/resourceBuildGraph.test.mjs",
		"scripts/test/assets/publishedAssetBoundary.test.mjs",
		"scripts/test/assets/generatedAssetMembership.test.mjs",
		"scripts/test/assets/npcModelPublication.test.mjs",
		"scripts/test/assets/boothModelAssets.test.mjs",
		"scripts/test/assets/cifSpriteCatalog.test.mjs",
		"scripts/test/assets/slotEffectPublication.test.mjs",
		"scripts/test/assets/skillPaneImageReferences.test.mjs",
		"scripts/test/world/worldMapImageReferences.test.mjs",
		"scripts/test/mission/effectProgramClosure.test.mjs",
		"scripts/test/assets/skillSpineAim.test.mjs",
		"scripts/test/assets/characterModelAssetContracts.test.mjs",
		"scripts/test/assets/actorLocomotionRootMotion.test.mjs",
		"scripts/test/assets/banTimeline.test.mjs",
		"scripts/test/assets/npcAnimationContactMarkers.test.mjs",
		"scripts/test/mission/missionMinimapBuild.test.mjs",
		"scripts/test/mission/minimapArtCoverage.test.mjs",
		"scripts/test/world/authoredWorldRegionResources.test.mjs",
		"scripts/test/assets/assetPackIntegrity.test.mjs",
		"scripts/test/assets/rebuildLockInterop.test.mjs",
		"scripts/test/assets/assetPackGroupParity.test.mjs",
		"scripts/test/assets/assetPackOwnership.test.mjs",
		"scripts/test/assets/imagePackOwnership.test.mjs",
		"scripts/test/assets/packGroupRefresh.test.mjs",
		"scripts/test/assets/worldMapPackOwnership.test.mjs",
		"scripts/test/assets/focusedCaseOwnership.test.mjs",
		"scripts/test/assets/mixedUiOwnership.test.mjs",
		"scripts/test/assets/pythonRun.test.mjs",
		"scripts/test/assets/optionalDataAsset.test.mjs",
		"scripts/test/assets/nameFilterAsset.test.mjs",
		"scripts/test/assets/backgroundInstallAsset.test.mjs",
		"scripts/test/assets/assetPackLayout.test.mjs",
		"scripts/test/assets/bsrAuthoredBox.test.mjs",
		"scripts/test/assets/englishCorrections.test.mjs",
		"scripts/test/assets/memberCompression.test.mjs",
		"scripts/test/assets/servedSize.test.mjs",
		"scripts/test/assets/minimapTextureParity.test.mjs",
		"scripts/test/assets/stallNetworkAssets.test.mjs",
		"scripts/test/assets/terrainBlockTextures.test.mjs"
	]
};
