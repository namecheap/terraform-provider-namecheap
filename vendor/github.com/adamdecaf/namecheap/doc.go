// namecheap - Golang client for Namecheap's API
//
// To use this project you'll need to either pull down the source
// or vendor it into your project.
//
// Once added to your project there are two ways to contruct a Client
//
//     namecheap.New() // reads environmental variables
//     namecheap.NewClient(username, apiuser string, token string, ip string, useSandbox)
//
// The following environmental variables are supported:
//
//     NAMECHEAP_USERNAME        Username: e.g. adamdecaf
//     NAMECHEAP_API_USER        ApiUser: e.g. adamdecaf
//     NAMECHEAP_TOKEN           From https://ap.www.namecheap.com/Profile/Tools/ApiAccess
//     NAMECHEAP_IP              Your IP (must be whitelisted)
//     NAMECHEAP_USE_SANDBOX     Use sandbox environment
//
// The public methods are viewable here: https://godoc.org/github.com/adamdecaf/namecheap
//
// Please raise an issue or pull request if you run into problems. Thanks!
package namecheap
