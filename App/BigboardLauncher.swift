import Bigboard
import Darwin

@main
enum BigboardLauncher {
    static func main() {
        if let status = BigboardCommandLine.dispatch(CommandLine.arguments) { exit(status) }
        BigboardApplication.main()
    }
}
