import { z } from "zod";

const slugRegex = /^[a-z0-9-]+$/;

export const loginSchema = z.object({
  email: z.string().email(),
  password: z.string().min(8)
});

export const passwordChangeSchema = z.object({
  currentPassword: z.string().min(8, "Enter your current password."),
  newPassword: z.string().min(12, "Use at least 12 characters for the new password."),
  confirmPassword: z.string().min(12, "Confirm the new password.")
}).superRefine((value, ctx) => {
  if (value.newPassword !== value.confirmPassword) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      path: ["confirmPassword"],
      message: "The new passwords do not match."
    });
  }
  if (value.currentPassword === value.newPassword) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      path: ["newPassword"],
      message: "Choose a new password instead of reusing the current one."
    });
  }
});

export const customerSchema = z.object({
  name: z.string().min(2),
  slug: z.string().min(2).regex(slugRegex),
  primaryContactEmail: z.string().email().optional().or(z.literal("")),
  plan: z.string().min(2),
  seats: z.coerce.number().int().min(1),
  mrrCents: z.coerce.number().int().min(0),
  notes: z.string().optional().or(z.literal(""))
});

export const organizationSchema = z.object({
  customerId: z.string().uuid(),
  name: z.string().min(2),
  slug: z.string().min(2).regex(slugRegex),
  externalOrgId: z.string().uuid().optional().or(z.literal("")),
  region: z.string().min(2),
  environment: z.enum(["production", "staging", "development"]),
  gatewayUrl: z.string().url().optional().or(z.literal(""))
});

export const reasonSchema = z.string().trim().min(10, "Please give a little more context.");

export const orgActionSchema = z.object({
  organizationId: z.string().uuid(),
  action: z.enum(["suspend", "reactivate", "terminate", "revoke_all_devices", "mark_contract_ended"]),
  reason: reasonSchema,
  confirmation: z.string().optional().or(z.literal(""))
});

export const deviceActionSchema = z.object({
  deviceId: z.string().uuid(),
  organizationId: z.string().uuid(),
  action: z.enum(["disable", "reactivate", "revoke"]),
  reason: reasonSchema
});

export const staffSchema = z.object({
  name: z.string().min(2),
  email: z.string().email(),
  role: z.enum(["owner", "admin", "operator", "viewer"]),
  password: z.string().min(10)
});

