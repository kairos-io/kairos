// Product name shown in the UI. Change it here to re-brand every label,
// heading and the browser tab title at once.
export const PRODUCT_NAME = "Kairos";

// documentTitle builds the browser tab title: "Nodes · Kairos" for a page,
// or just the product name when no page is given.
export function documentTitle(page?: string): string {
  const name = page?.trim();
  return name ? `${name} · ${PRODUCT_NAME}` : PRODUCT_NAME;
}
