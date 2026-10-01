import { isIP } from "node:net";

export interface AdmissionLimits {
  maxRegistrations: number;
  enrollmentsPerDay: number;
}

function setting(name: string, fallback: number, max: number): number {
  const value = Number(process.env[name] ?? fallback);
  if (!Number.isInteger(value) || value < 1 || value > max) throw new Error(`${name} must be 1 to ${max}.`);
  return value;
}

export const admissionLimits: AdmissionLimits = {
  maxRegistrations: setting("COMPUTER_DIRECTORY_MAX_REGISTRATIONS", 100_000, 10_000_000),
  enrollmentsPerDay: setting("COMPUTER_DIRECTORY_ENROLLMENTS_PER_DAY", 100, 10_000),
};

/** IPv6 privacy-address rotation must not reset admission limits within a /64. */
export function sourceNetwork(address: string): string {
  const normalized = address.toLowerCase().split("%", 1)[0];
  if (isIP(normalized) === 4) return normalized;
  if (isIP(normalized) !== 6) return "unknown";
  const lastColon = normalized.lastIndexOf(":");
  const tail = normalized.slice(lastColon + 1);
  const ip = tail.includes(".")
    ? (() => {
        const bytes = tail.split(".").map(Number);
        return (
          normalized.slice(0, lastColon + 1) +
          ((bytes[0] << 8) | bytes[1]).toString(16) +
          ":" +
          ((bytes[2] << 8) | bytes[3]).toString(16)
        );
      })()
    : normalized;
  const halves = ip.split("::");
  const left = halves[0] ? halves[0].split(":") : [];
  const right = halves[1] ? halves[1].split(":") : [];
  const words = (
    halves.length === 1 ? left : [...left, ...Array(8 - left.length - right.length).fill("0"), ...right]
  ).map((word) => parseInt(word, 16));
  if (words.slice(0, 5).every((word) => word === 0) && words[5] === 0xffff)
    return [words[6] >> 8, words[6] & 255, words[7] >> 8, words[7] & 255].join(".");
  return (
    words
      .slice(0, 4)
      .map((word) => word.toString(16).padStart(4, "0"))
      .join(":") + "::/64"
  );
}
