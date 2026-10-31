import type { Metadata } from "next";
import ReviewBar from "@/components/design-lab/ReviewBar";

export const metadata: Metadata = {
  title: "Mindwire — Design studies",
  robots: { index: false, follow: false },
};

export default function DesignLabLayout({ children }: { children: React.ReactNode }) {
  return <><ReviewBar />{children}</>;
}
