namespace Shop.Text;

// CS-G1: the pinned grammar reads this C# 14 extension block without an
// error as a constructor named `extension` whose members are local
// functions. Its name differs from the type's, so nothing in it is claimed.
public static class StringExtensions
{
    extension(string s)
    {
        public bool IsBlank() => s.Trim().Length == 0;
    }

    public static bool IsShort(this string s) => s.Length < 4;   // CS-F1 (claimed)
}
