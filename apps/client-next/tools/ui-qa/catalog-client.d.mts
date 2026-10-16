/*
===========================================================================
catalog-client.d.mts - shared fresh catalogue fetch contract
===========================================================================
*/
import type { Catalog } from "./catalog-types";
export function fetchCatalog( request?: typeof fetch ): Promise<Catalog>;
export function fetchCatalogIdentity( request?: typeof fetch ): Promise<Catalog["identity"]>;
