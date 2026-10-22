//go:build production

package shim

// DevServerURL is always "" in a production build: a released binary
// serves its embedded frontend, whatever the environment says.
func DevServerURL() string { return "" }
