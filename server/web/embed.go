package web

import "io/fs"

// StaticFS holds the frontend static files.
// In Docker builds (tag "docker"), files are embedded from static/.
// In local builds, this is nil and the frontend is not served.
var StaticFS fs.FS
