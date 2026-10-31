import type { Metadata } from "next";
import RefinedLanding from "@/components/design-lab/refined/RefinedLanding";
import { heroHeadline } from "@/lib/hero";

export const metadata: Metadata = {
  title: `Mindwire — ${heroHeadline.join(" ")}`,
  description:
    "Add agents that reason, create, and take action inside your product. White-label leading agent harnesses with Mindwire, without rebuilding the agent layer.",
  alternates: { canonical: "/" },
};

export default function Home() {
  return <RefinedLanding heroVariant="layers" />;
}
