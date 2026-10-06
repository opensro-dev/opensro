/*
===========================================================================
history.go - bounded claimed identity on refused authentication attempts
===========================================================================
*/
package agentserver

/*
================
boundedLoginName
================
*/
func boundedLoginName(name string) string {
	if len(name) > 64 {
		return name[:64]
	}
	return name
}
