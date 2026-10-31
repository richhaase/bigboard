// swift-tools-version: 6.0
import PackageDescription
let package = Package(name: "Bigboard", platforms: [.macOS("27.0")], products: [.executable(name: "Bigboard", targets: ["Bigboard"])], targets: [.executableTarget(name: "Bigboard", path: "Sources"), .testTarget(name: "BigboardTests", dependencies: ["Bigboard"], path: "Tests")])
