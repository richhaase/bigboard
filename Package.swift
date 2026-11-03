// swift-tools-version: 6.0
import PackageDescription
let package = Package(
    name: "Bigboard", platforms: [.macOS("26.0")],
    products: [.library(name: "Bigboard", targets: ["Bigboard"])],
    targets: [
        .target(name: "Bigboard", path: "Sources"),
        .testTarget(name: "BigboardTests", dependencies: ["Bigboard"], path: "Tests"),
    ])
