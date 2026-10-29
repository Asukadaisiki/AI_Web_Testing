import { z } from "zod/v4";

export const ControlInputSchema = z.discriminatedUnion("action", [
  z.strictObject({
    action: z.literal("click"),
    x: z.number().int().nonnegative().max(1279),
    y: z.number().int().nonnegative().max(799),
  }),
  z.strictObject({
    action: z.literal("type"),
    text: z
      .string()
      .min(1)
      .max(2000)
      .refine((text) => !/[\x00-\x1f\x7f-\x9f]/.test(text)),
  }),
  z.strictObject({
    action: z.literal("key"),
    key: z.enum([
      "Enter",
      "Tab",
      "Escape",
      "Backspace",
      "ArrowUp",
      "ArrowDown",
      "ArrowLeft",
      "ArrowRight",
    ]),
  }),
  z.strictObject({
    action: z.literal("navigate"),
    url: z
      .url()
      .max(2000)
      .refine((url) => {
        const parsed = new URL(url);
        return (
          ["http:", "https:"].includes(parsed.protocol) &&
          !parsed.username &&
          !parsed.password
        );
      }),
  }),
]);
