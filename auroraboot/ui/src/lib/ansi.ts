import Convert from "ansi-to-html";

const converter = new Convert({
  fg: "#e2e8f0",
  bg: "transparent",
  newline: true,
  escapeXML: true,
});

export function ansiToHtml(text: string): string {
  if (!text) return "";
  return converter.toHtml(text);
}
