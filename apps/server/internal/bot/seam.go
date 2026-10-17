/*
===========================================================================

seam.go - the interface between the bot brain and the connection

The bot brain must not know the connection. Phase 2 owns the seam in
brain.go: the brain reads the world through WorldView and gives back actions
through Actions. The service adapts the socket to both. The same brain can
control a character of a player in a later task.

===========================================================================
*/
package bot
