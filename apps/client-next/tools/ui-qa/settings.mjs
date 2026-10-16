/*
===========================================================================
settings.mjs - one loopback endpoint for the review server and its probes

Keep this separate from the operator console, which uses port 5190.
===========================================================================
*/
export const UI_QA_PORT = 5191;
export const UI_QA_ORIGIN = `http://127.0.0.1:${UI_QA_PORT}`;
export const UI_QA_CATALOG_PATH = "/__uiqa/catalog";
