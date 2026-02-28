package server

// RedactSensitiveArgs masks command arguments that may contain credentials.
func RedactSensitiveArgs(command string, args []string) []string {
	return redactSensitiveArgs(command, args)
}

func redactSensitiveArgs(command string, args []string) []string {
	redacted := append([]string(nil), args...)
	if command != "openclaw" {
		return redacted
	}
	if len(redacted) >= 4 && redacted[0] == "config" && redacted[1] == "set" {
		redacted[3] = "******"
	}
	return redacted
}
