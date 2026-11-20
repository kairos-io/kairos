import { Info } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

// InfoTooltip renders a small ⓘ button next to a label that opens a
// high-contrast popover with help text. The popover is:
//
// 1. Hoverable with the mouse — and importantly, moving from the button
//    into the popover keeps it open. This is implemented via a 150 ms
//    close timer that any of the subscribed elements (button + popover)
//    can cancel on mouseEnter. The classic "tooltip disappears mid-move"
//    bug is what happens when only the button handles mouseLeave.
// 2. Click-toggleable for keyboard and touch users. Clicking outside
//    closes it.
// 3. High-contrast (dark background + light text) so it's visually
//    distinct from the page it's overlaying — the previous white-on-white
//    popover variant was near-invisible on several pages.
//
// Children can contain JSX anchors; because the popover stays open while
// the pointer is over it, those anchors are actually clickable.
export function InfoTooltip({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  const wrapperRef = useRef<HTMLSpanElement>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const cancelClose = useCallback(() => {
    if (closeTimer.current) {
      clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }
  }, []);

  const scheduleClose = useCallback(() => {
    cancelClose();
    closeTimer.current = setTimeout(() => setOpen(false), 150);
  }, [cancelClose]);

  // Click-outside dismissal — keeps the click-to-open path working for
  // keyboard users who don't hover.
  useEffect(() => {
    if (!open) return;
    function handleClick(e: MouseEvent) {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  }, [open]);

  // Clean up any pending timer on unmount so it doesn't fire into a
  // dead component.
  useEffect(() => () => cancelClose(), [cancelClose]);

  return (
    <span
      ref={wrapperRef}
      className="relative inline-flex"
      onMouseEnter={cancelClose}
      onMouseLeave={scheduleClose}
    >
      <button
        type="button"
        className="inline-flex ml-1 text-muted-foreground hover:text-foreground align-middle focus:outline-none focus-visible:text-foreground"
        aria-label="More information"
        aria-expanded={open}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setOpen((prev) => !prev);
        }}
        onMouseEnter={() => {
          cancelClose();
          setOpen(true);
        }}
      >
        <Info className="h-3.5 w-3.5" />
      </button>
      {open && (
        <div
          role="tooltip"
          // Anchor the popover to the button's LEFT edge instead of
          // centering it — centering blew half the tooltip off the
          // viewport when the button sat near the left margin (labels
          // on the Source step). A -2 offset nudges it a pinch left so
          // the arrow lines up with the icon. The popover always grows
          // rightward into the form area, where there's plenty of
          // space.
          className="absolute z-50 bottom-full left-0 -translate-x-2 mb-2 w-max max-w-sm rounded-md border bg-popover px-3 py-2.5 text-xs leading-relaxed text-popover-foreground shadow-lg"
          // Tooltip content links inherit underline + color-on-hover so
          // they're obviously clickable against the popover background.
          onMouseEnter={cancelClose}
          onMouseLeave={scheduleClose}
        >
          <div className="[&_a]:text-primary [&_a]:underline [&_a]:underline-offset-2 [&_a:hover]:text-primary-hover [&_code]:rounded [&_code]:bg-muted [&_code]:px-1 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-[11px]">
            {children}
          </div>
          <div
            className="absolute top-full left-3 -mt-px border-4 border-transparent border-t-popover"
            aria-hidden="true"
          />
        </div>
      )}
    </span>
  );
}
