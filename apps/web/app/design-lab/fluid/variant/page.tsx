import type { Metadata } from "next";
import RefinedLanding from "@/components/design-lab/refined/RefinedLanding";

export const metadata: Metadata = {
  title: "Mindwire — Optical layer hero study",
};

export default function HeroVariantStudy() {
  return <RefinedLanding heroVariant="layers" />;
}
