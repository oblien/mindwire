/** Public hosted-console URL, embedded in the marketing site at build time. */
export const consoleUrl =
  process.env.NEXT_PUBLIC_CONSOLE_URL?.trim() || "https://console.mindwire.sh";
