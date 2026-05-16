package c2

// ExecuteRemoteLock locks the interactive session (platform-specific).
func ExecuteRemoteLock() error {
	return executeLockWorkstation()
}
