import type { Metadata } from "next";
import RefinedLanding from "@/components/design-lab/refined/RefinedLanding";
import { heroHeadline } from "@/lib/hero";

export const metadata: Metadata = {
  title: `MindWire — ${heroHeadline.join(" ")}`,
  description:
    "Add agents that reason, create, and take action inside your product. White-label leading agent harnesses with MindWire, without rebuilding the agent layer.",
  alternates: { canonical: "/" },
};

export default function Home() {
  return <RefinedLanding heroVariant="layers" />;
}
