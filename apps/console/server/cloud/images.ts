import type { Oblien } from "oblien";
import { COMPUTER_PLANS, computerPlanFitsImage } from "../../shared/cloud";

type Image = Awaited<ReturnType<Oblien["workspaces"]["images"]["list"]>>["images"][number];

/** Only a priced image that fits a published computer can enter checkout. */
export function monthlyComputerImage(image: Image): boolean {
  if (!["linux", "macos"].includes(image.base_os?.family ?? "")) return false;
  return COMPUTER_PLANS.some(plan => computerPlanFitsImage(plan, image));
}
