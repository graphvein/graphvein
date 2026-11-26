export type Json = string | number | boolean | null | { [key: string]: Json | undefined } | Json[]

// Written by hand in the generator's style: the optional Row property is
// not a form the generator writes, so the file is no schema source.
export type Database = {
  public: {
    Tables: {
      notes: {
        Row: {
          id: string
          body?: string
        }
        Insert: {
          id: string
          body?: string
        }
        Update: {
          id?: string
          body?: string
        }
        Relationships: []
      }
    }
    Views: {
      [_ in never]: never
    }
    Functions: {
      [_ in never]: never
    }
    Enums: {
      [_ in never]: never
    }
    CompositeTypes: {
      [_ in never]: never
    }
  }
}
