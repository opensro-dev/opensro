# Server-driven pet UI — live verification (#2677)

Tested in a real browser against matched Agent/GameWorld builds from main b345aa88 plus the issue2677 patch. The existing live installation used another protocol and authority schema, so testing used a disposable copy upgraded from schema35 to36. The original database was restored before these tests and was not migrated.

| Case | Observed result |
| --- | --- |
| Server expansion on; saved client attackPetTraining=false | Existing level90 wolf automatically requested its state. Skills tree, Reset control and three gear slots were available. The HP window used the trained pool (35100 for this fixture). |
| Server expansion off; same browser and pet | After reconnect, Inventory and Setting tabs were natively disabled; no pet skills page appeared. The HP window used the native reference pool (39343). No disabled-training trainer message appeared. |
| Original data | All five original character records, inventories and pet gear matched the pre-test backup exactly; ground items also matched, and SQLite quick_check passed. |

The new server and client flag behavior required no further production-code fix during this live test. Skill combat and drop generation were not retested here: automatic native-off drop gating across all degrees/slots and on/off state responses are covered by the regression suite. The obsolete Experimental key is absent from runtime source and cannot override the server.

Focused tests:56pass. Client source checks, ownership/capability/execution checks and the server changed-package gate pass. The full asset-dependent suite has seven failures reproduced on untouched b345aa88 with the same generated assets; this report does not claim that suite is green.

Original runtime jobs and settings were restored, including the pre-test binaries and authority path. The temporary client was stopped. Screenshots and disposable test databases remain local and are not committed.
