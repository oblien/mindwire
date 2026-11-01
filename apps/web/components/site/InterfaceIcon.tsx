import type { CSSProperties, HTMLAttributes } from "react";
import styles from "./interface-icon.module.css";

type IconProps = HTMLAttributes<HTMLSpanElement> & {
  size?: number;
  strokeWidth?: number;
};

function icon(name: string) {
  return function InterfaceIcon({
    size = 20,
    strokeWidth: _strokeWidth,
    className = "",
    style,
    ...props
  }: IconProps) {
    return (
      <span
        aria-hidden="true"
        {...props}
        data-site-icon={name}
        className={`${styles.icon} ${className}`}
        style={
          {
            "--icon-size": `${size}px`,
            "--icon-image": `url("/icons/interface/${name}.png")`,
            ...style,
          } as CSSProperties
        }
      />
    );
  };
}

export const ArrowRight = icon("arrow-right");
export const ArrowDown = icon("arrow-down");
export const ArrowUpRight = icon("arrow-up-right");
export const Check = icon("check");
export const Copy = icon("copy");
export const Play = icon("play");
export const Pause = icon("pause");
export const RotateCcw = icon("refresh");
export const Menu = icon("menu");
export const X = icon("close");
export const ChevronDown = icon("chevron-down");
export const FileText = icon("document");
export const Folder = icon("folder");
export const FileCode2 = icon("code");
export const GitBranch = icon("branch");
export const Plus = icon("plus");
export const Clock = icon("clock");
export const Cloud = icon("cloud");
export const Bolt = icon("bolt");
export const Processor = icon("processor");
export const Brain = icon("brain");
export const Search = icon("search");
export const Monitor = icon("monitor");
export const Send = icon("send");
export const MessageSquare = icon("message");
export const LinkIcon = icon("link");
export const Shield = icon("shield");
export const Bot = icon("bot");

export const CalendarDays = icon("calendar");
export const Info = icon("info");
export const Terminal = icon("terminal");
export const KeyRound = icon("key");
export const GitCommitHorizontal = icon("commit");
export const Files = icon("documents");
export const Smartphone = icon("smartphone");
export const Network = icon("network");
export const Laptop = Monitor;
export const Code2 = FileCode2;
export const ShieldCheck = Shield;
