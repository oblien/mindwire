import { useState } from "react";
import { Check, ChevronDown, Laptop, Terminal } from "lucide-react";
import { computerPlanFitsImage, type CloudImage, type ComputerPlan } from "@shared/cloud";
import { primaryComputerImages } from "@/lib/computer-images";
import { cn } from "@/lib/utils";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { money } from "./portal/common";

export function ComputerSystemPicker({ images, plans, value, onChange, disabled }: {
  images: CloudImage[];
  plans: ComputerPlan[];
  value: string;
  onChange: (id: string) => void;
  disabled?: boolean;
}) {
  const primary = primaryComputerImages(images);
  const others = images.filter((image) => image.id !== primary.linux?.id && image.id !== primary.mac?.id);
  const otherSelected = others.some((image) => image.id === value);
  const [otherOpen, setOtherOpen] = useState<boolean | undefined>();
  const showOthers = otherOpen ?? otherSelected;
  const choices = [
    { name: "Linux", image: primary.linux, Icon: Terminal },
    { name: "Mac", image: primary.mac, Icon: Laptop },
  ];

  return (
    <fieldset disabled={disabled} className="space-y-4">
      <legend className="mb-4 text-sm font-medium">Operating system</legend>
      <div className="grid grid-cols-2 gap-3 sm:gap-4">
        {choices.map(({ name, image, Icon }) => {
          const selected = !!image && image.id === value;
          const minimumPlan = image && plans.find((plan) => computerPlanFitsImage(plan, image));
          return (
            <label key={name} className={cn(
              "relative flex min-w-0 flex-col gap-5 rounded-2xl border p-5 transition-colors has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-4 has-[:focus-visible]:outline-ring",
              selected ? "border-foreground/70 bg-[var(--mw-peach-soft)]" : "border-border bg-card hover:bg-secondary",
              !image || disabled ? "cursor-default opacity-50" : "cursor-pointer",
            )}>
              <input type="radio" name="computer-system" value={image?.id ?? name} checked={selected}
                disabled={!image || disabled} className="sr-only"
                onChange={() => { if (image) { setOtherOpen(false); onChange(image.id); } }} />
              <div className="flex items-center justify-between">
                <Icon className="size-7" aria-hidden="true" />
                <span className={cn("grid size-5 place-items-center rounded-full border", selected ? "border-foreground bg-foreground text-background" : "border-muted-foreground/35")} aria-hidden="true">
                  {selected && <Check className="size-3" />}
                </span>
              </div>
              <div className="space-y-1">
                <p className="text-lg font-medium">{name}</p>
                <p className="truncate text-xs text-muted-foreground">{image ? image.label || image.id : "Currently unavailable"}</p>
              </div>
              {minimumPlan && <p className="text-xs text-muted-foreground">From {money(minimumPlan.monthlyCents)}/month</p>}
            </label>
          );
        })}
      </div>
      {others.length > 0 && (
        <div className="space-y-3">
          <button type="button" className="flex items-center gap-2 py-1 text-sm text-muted-foreground hover:text-foreground"
            aria-expanded={showOthers} onClick={() => setOtherOpen(!showOthers)}>
            Other systems
            <ChevronDown className={cn("size-4 transition-transform", showOthers && "rotate-180")} />
          </button>
          {showOthers && (
            <Select value={otherSelected ? value : ""} onValueChange={onChange} disabled={disabled}>
              <SelectTrigger aria-label="Other operating systems"><SelectValue placeholder="Choose another system" /></SelectTrigger>
              <SelectContent>
                {others.map((image) => <SelectItem key={image.id} value={image.id}>{image.label || image.id}</SelectItem>)}
              </SelectContent>
            </Select>
          )}
        </div>
      )}
    </fieldset>
  );
}
