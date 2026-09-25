// Independent native decoder: check the half-block terminal rendering, including
// its quiet zone, rather than comparing it to the encoder's own matrix.
import Foundation
import CoreGraphics
import Vision

struct Fixture: Decodable { let payload: String; let lines: [String] }
let directory = URL(fileURLWithPath: CommandLine.arguments[1])
let fixture = try JSONDecoder().decode(Fixture.self, from: Data(contentsOf: directory.appendingPathComponent("fixture.json")))
let rows = fixture.lines.map(Array.init)
let scale = 8, width = rows[0].count * scale, height = rows.count * 2 * scale
var pixels = [UInt8](repeating: 255, count: width * height)
for y in 0..<height {
    for x in 0..<width {
        let block = rows[y / (2 * scale)][x / scale]
        let top = (y / scale) % 2 == 0
        let black = block == "█" || (top && block == "▀") || (!top && block == "▄")
        pixels[y * width + x] = black ? 0 : 255
    }
}
let provider = CGDataProvider(data: Data(pixels) as CFData)!
let image = CGImage(width: width, height: height, bitsPerComponent: 8, bitsPerPixel: 8,
                    bytesPerRow: width, space: CGColorSpaceCreateDeviceGray(), bitmapInfo: CGBitmapInfo(rawValue: 0),
                    provider: provider, decode: nil, shouldInterpolate: false, intent: .defaultIntent)!
for handler in [VNImageRequestHandler(cgImage: image),
                VNImageRequestHandler(url: directory.appendingPathComponent("browser.png"))] {
    let request = VNDetectBarcodesRequest()
    request.symbologies = [.qr]
    try handler.perform([request])
    guard request.results?.count == 1, request.results?.first?.payloadStringValue == fixture.payload else {
        fatalError("Native QR decode did not reproduce the complete invitation")
    }
}
print("Terminal and browser QR decoded successfully.")
