// Decode the terminal glyphs actually emitted by the running daemon.
import CoreImage
import Foundation

let lines = String(decoding: FileHandle.standardInput.readDataToEndOfFile(), as: UTF8.self).split(
  separator: "\n")
guard let printed = lines.last, let width = lines.first?.count, width > 0 else { exit(1) }
let scale = 6
var pixels = Data()
for line in lines.dropLast() {
  guard line.count == width else { exit(1) }
  for lower in [false, true] {
    var row = Data()
    for glyph in line {
      let white = glyph == "█" || glyph == (lower ? "▄" : "▀")
      row.append(contentsOf: repeatElement(UInt8(white ? 255 : 0), count: scale))
    }
    for _ in 0..<scale { pixels.append(row) }
  }
}
let image = CIImage(
  bitmapData: pixels, bytesPerRow: width * scale,
  size: CGSize(width: width * scale, height: pixels.count / (width * scale)),
  format: .L8, colorSpace: CGColorSpaceCreateDeviceGray())
let detector = CIDetector(
  ofType: CIDetectorTypeQRCode, context: nil,
  options: [CIDetectorAccuracy: CIDetectorAccuracyHigh])
guard let code = detector?.features(in: image).first as? CIQRCodeFeature,
  let url = code.messageString, url == printed
else { exit(2) }
print(url)
