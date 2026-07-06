package version

// Version is a variable so release builds can inject a tag with -ldflags -X.
var Version = "0.2.0-dev"

const Runtime = "go"
