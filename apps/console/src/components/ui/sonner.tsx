import { Toaster as Sonner, type ToasterProps } from "sonner";

// The app is dark-first (index.html ships `class="dark"`); read the live theme off <html> so toasts
// track a future light toggle without pulling in a theme provider.
function currentTheme(): ToasterProps["theme"] {
  if (typeof document !== "undefined" && document.documentElement.classList.contains("dark")) {
    return "dark";
  }
  return "light";
}

function Toaster(props: ToasterProps) {
  return (
    <Sonner
      theme={currentTheme()}
      className="toaster group"
      toastOptions={{
        classNames: {
          toast:
            "group toast !rounded-none !border !border-border !bg-popover !text-popover-foreground !shadow-md",
          description: "!text-muted-foreground",
          actionButton: "!bg-primary !text-primary-foreground",
          cancelButton: "!bg-muted !text-muted-foreground",
        },
      }}
      {...props}
    />
  );
}

export { Toaster };
export { toast } from "sonner";
