-- Add missing saga_status enum values (idempotent)
DO $$ BEGIN
    ALTER TYPE saga_status ADD VALUE IF NOT EXISTS 'COMPENSATING';
EXCEPTION WHEN duplicate_object THEN null;
END $$;

DO $$ BEGIN
    ALTER TYPE saga_status ADD VALUE IF NOT EXISTS 'COMPENSATION_FAILED';
EXCEPTION WHEN duplicate_object THEN null;
END $$;

-- Normalise order_items: ensure item_id exists and product_id is removed
DO $$
DECLARE
    has_product_id boolean;
    has_item_id    boolean;
BEGIN
    SELECT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'order_items' AND column_name = 'product_id'
    ) INTO has_product_id;

    SELECT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'order_items' AND column_name = 'item_id'
    ) INTO has_item_id;

    IF has_product_id AND NOT has_item_id THEN
        ALTER TABLE order_items RENAME COLUMN product_id TO item_id;
    ELSIF has_product_id AND has_item_id THEN
        ALTER TABLE order_items DROP COLUMN product_id;
    END IF;
END $$;
