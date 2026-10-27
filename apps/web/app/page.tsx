import type { Metadata } from "next";
import FrameworkHome from "@/components/landing/FrameworkHome";

export const metadata: Metadata = {
  title: "Mindwire — Your product. Powered by agents.",
  description:
    "Add agents that reason, create, and take action inside your product. White-label leading agent harnesses with Mindwire, without rebuilding the agent layer.",
  alternates: { canonical: "/" },
};

export default function Home() {
  return <FrameworkHome />;
}
