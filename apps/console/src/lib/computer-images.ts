import type { CloudImage } from "@shared/cloud";

/** The simple choices are base systems; software presets stay under Other. */
export function primaryComputerImages(images: CloudImage[]) {
  const linux = images.filter((image) => image.base_os?.family === "linux");
  const ubuntu = linux.filter((image) => image.base_os?.distribution?.toLowerCase() === "ubuntu");
  const mac = images.filter((image) => image.base_os?.family === "macos");
  const base = (entries: CloudImage[]) => entries.find((image) => !image.preset) ?? entries[0];
  return { linux: base(ubuntu) ?? base(linux), mac: base(mac) };
}
