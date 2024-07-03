//go:build windows
// +build windows

package xmqd

// On Windows, file names cannot contain colons.
func getBackendName(topicName, channelName string) string {
	// backend names, for uniqueness, automatically include the topic... <topic>;<channel>
	backendName := topicName + ";" + channelName
	return backendName
}
