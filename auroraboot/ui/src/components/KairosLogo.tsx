interface KairosLogoProps {
  className?: string;
}

// KairosLogo renders the official Kairos-by-SpectroCloud vertical wordmark.
// The asset lives in ui/public/ so Vite serves it unchanged, and the path
// is stable across dev and production builds.
export function KairosLogo({ className }: KairosLogoProps) {
  return (
    <img
      src="/kairos-wordmark.png"
      alt="Kairos by SpectroCloud"
      className={className}
      draggable={false}
    />
  );
}
