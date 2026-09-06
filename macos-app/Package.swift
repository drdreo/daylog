// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "Daylog",
    platforms: [.macOS(.v13)],
    products: [.executable(name: "Daylog", targets: ["DaylogApp"])],
    targets: [
        .target(name: "DaylogCore"),
        .executableTarget(name: "DaylogApp", dependencies: ["DaylogCore"]),
        // Executable checks also run with Command Line Tools alone (no XCTest/Xcode).
        .executableTarget(name: "DaylogCoreChecks", dependencies: ["DaylogCore"], path: "Tests/DaylogCoreTests")
    ]
)
