// Package update, anitr-cli'nin güncellemeleriyle ilgili bilgileri içerir.
package update

import "fmt"

// GithubRepo is the repository URL, anitr-cli projesinin GitHub üzerindeki kullanıcı/ad şeklindeki yolu.
var GithubRepo string = "prayjofir/anitr-cli"

// githubAPI is the URL to fetch the latest release information from the GitHub API.
var githubAPI string = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", GithubRepo)

// repoLink is the link to the project's GitHub page.
var repoLink string = fmt.Sprintf("https://github.com/%s", GithubRepo)

// -v/--version bilgisi için
var version string = "dev"
var buildEnv string = "unknown"
