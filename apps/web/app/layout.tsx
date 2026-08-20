import type { Metadata } from "next";
import { GeistSans } from "geist/font/sans";
import { GeistMono } from "geist/font/mono";
import { RootProvider } from "fumadocs-ui/provider/next";
import "./globals.css";
import Nav from "@/components/Nav";
import Footer from "@/components/Footer";
import HideOnDocs from "@/components/HideOnDocs";

export const metadata: Metadata = {
  title: "MindWire — wire in the best coding agents",
  description:
    "The world's best coding agents already do the hard part. MindWire is the runtime that drives them inside your product — on your own machine, behind one protocol you can swap agents under.",
  metadataBase: new URL("https://mindwire.sh"),
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`${GeistSans.variable} ${GeistMono.variable}`} suppressHydrationWarning>
      <body className="font-sans">
        {/* RootProvider owns theme (next-themes, class strategy → the same `.dark` contract every
            marketing page already uses) and the search dialog. It wraps Nav so its ThemeToggle and the
            ⌘K search reach the provider. Default light, opt-in dark — matching the prior behavior. */}
        <RootProvider theme={{ defaultTheme: "light", enableSystem: false }}>
          <Nav />
          <main className="relative">{children}</main>
          {/* The marketing footer is hidden on /docs — the Fumadocs sidebar is a fixed full-height
              column, so a site footer beneath it just adds clutter. */}
          <HideOnDocs>
            <Footer />
          </HideOnDocs>
        </RootProvider>
      </body>
    </html>
  );
}
