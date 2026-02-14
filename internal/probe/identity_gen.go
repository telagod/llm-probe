package probe

import (
	"fmt"
	"math"
	"math/big"
	"math/rand"
)

// identityTemplate generates a fresh identityCase with random parameters.
type identityTemplate struct {
	Tier string
	Gen  func(rng *rand.Rand) identityCase
}

// generateIdentityCases picks count templates per tier and generates fresh cases.
func generateIdentityCases(perTier int, seed int64) []identityCase {
	rng := rand.New(rand.NewSource(seed))
	byTier := map[string][]identityTemplate{}
	for _, t := range identityTemplates {
		byTier[t.Tier] = append(byTier[t.Tier], t)
	}
	var cases []identityCase
	counters := map[string]int{}
	for _, tier := range []string{"easy", "medium", "hard"} {
		pool := byTier[tier]
		rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
		n := perTier
		if n > len(pool) {
			n = len(pool)
		}
		for i := 0; i < n; i++ {
			counters[tier]++
			c := pool[i].Gen(rng)
			c.Tier = tier
			c.ID = fmt.Sprintf("%s%d", tier[:1], counters[tier])
			cases = append(cases, c)
		}
	}
	return cases
}

// --- helpers ---

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}

func factorial(n int) int {
	r := 1
	for i := 2; i <= n; i++ {
		r *= i
	}
	return r
}

func comb(n, k int) int {
	if k > n || k < 0 {
		return 0
	}
	return factorial(n) / (factorial(k) * factorial(n-k))
}

func isPrime(n int) bool {
	if n < 2 {
		return false
	}
	for i := 2; i*i <= n; i++ {
		if n%i == 0 {
			return false
		}
	}
	return true
}

func eulerTotient(n int) int {
	result := n
	p := 2
	temp := n
	for p*p <= temp {
		if temp%p == 0 {
			for temp%p == 0 {
				temp /= p
			}
			result -= result / p
		}
		p++
	}
	if temp > 1 {
		result -= result / temp
	}
	return result
}

func modPow(base, exp, mod int) int {
	return int(new(big.Int).Exp(big.NewInt(int64(base)), big.NewInt(int64(exp)), big.NewInt(int64(mod))).Int64())
}

func det3x3(m [3][3]int) int {
	return m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) -
		m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) +
		m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
}

func formatFrac(num, den int) string {
	if den < 0 {
		num, den = -num, -den
	}
	g := gcd(abs(num), abs(den))
	num /= g
	den /= g
	if den == 1 {
		return fmt.Sprintf("%d", num)
	}
	return fmt.Sprintf("%d/%d", num, den)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// --- EASY templates (17) ---

var easyTemplates = []identityTemplate{
	// E1: a * b
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(90) + 11 // 11-100
		b := rng.Intn(90) + 11
		return identityCase{
			Question: fmt.Sprintf("What is %d * %d?", a, b),
			Expected: fmt.Sprintf("%d", a*b),
		}
	}},
	// E2: a + b + c
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(900) + 100
		b := rng.Intn(900) + 100
		c := rng.Intn(900) + 100
		return identityCase{
			Question: fmt.Sprintf("What is %d + %d + %d?", a, b, c),
			Expected: fmt.Sprintf("%d", a+b+c),
		}
	}},
	// E3: a^n
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(9) + 2  // 2-10
		n := rng.Intn(4) + 2  // 2-5
		result := 1
		for i := 0; i < n; i++ {
			result *= a
		}
		return identityCase{
			Question: fmt.Sprintf("What is %d raised to the power of %d?", a, n),
			Expected: fmt.Sprintf("%d", result),
		}
	}},
	// E4: percentage
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		pct := (rng.Intn(19) + 1) * 5 // 5,10,...,95
		base := (rng.Intn(19) + 1) * 50 // 50,100,...,950
		ans := pct * base / 100
		return identityCase{
			Question: fmt.Sprintf("What is %d%% of %d?", pct, base),
			Expected: fmt.Sprintf("%d", ans),
		}
	}},
	// E5: speed*time=distance
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		speed := (rng.Intn(18) + 3) * 10 // 30-200
		hours := rng.Intn(8) + 2          // 2-9
		return identityCase{
			Question: fmt.Sprintf("A car travels at %d km/h for %d hours. How many km does it travel?", speed, hours),
			Expected: fmt.Sprintf("%d", speed*hours),
		}
	}},
	// E6: integer division + remainder
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		divisor := rng.Intn(8) + 3 // 3-10
		quotient := rng.Intn(90) + 10
		remainder := rng.Intn(divisor)
		dividend := quotient*divisor + remainder
		return identityCase{
			Question: fmt.Sprintf("What is the remainder when %d is divided by %d?", dividend, divisor),
			Expected: fmt.Sprintf("%d", remainder),
		}
	}},
	// E7: square root of perfect square
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(30) + 4 // 4-33
		return identityCase{
			Question: fmt.Sprintf("What is the square root of %d?", n*n),
			Expected: fmt.Sprintf("%d", n),
		}
	}},
	// E8: unit conversion hours→minutes
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		h := rng.Intn(10) + 2
		m := rng.Intn(50) + 5
		return identityCase{
			Question: fmt.Sprintf("Convert %d hours and %d minutes to total minutes.", h, m),
			Expected: fmt.Sprintf("%d", h*60+m),
		}
	}},
	// E9: absolute value of difference
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(500) + 100
		b := rng.Intn(500) + 100
		d := a - b
		if d < 0 {
			d = -d
		}
		return identityCase{
			Question: fmt.Sprintf("What is the absolute value of %d - %d?", a, b),
			Expected: fmt.Sprintf("%d", d),
		}
	}},
	// E10: area of rectangle
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		w := rng.Intn(30) + 5
		h := rng.Intn(30) + 5
		return identityCase{
			Question: fmt.Sprintf("What is the area of a rectangle with width %d and height %d?", w, h),
			Expected: fmt.Sprintf("%d", w*h),
		}
	}},
	// E11: average of 3 integers (guaranteed divisible)
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		avg := rng.Intn(80) + 10
		d1 := rng.Intn(20) - 10
		d2 := rng.Intn(20) - 10
		a, b, c := avg+d1, avg+d2, avg-d1-d2
		return identityCase{
			Question: fmt.Sprintf("What is the average of %d, %d, and %d?", a, b, c),
			Expected: fmt.Sprintf("%d", avg),
		}
	}},
	// E12: decimal to binary
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(120) + 8 // 8-127
		return identityCase{
			Question: fmt.Sprintf("Convert the decimal number %d to binary.", n),
			Expected: fmt.Sprintf("%b", n),
		}
	}},
	// E13: perimeter of rectangle
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		w := rng.Intn(40) + 3
		h := rng.Intn(40) + 3
		return identityCase{
			Question: fmt.Sprintf("What is the perimeter of a rectangle with sides %d and %d?", w, h),
			Expected: fmt.Sprintf("%d", 2*(w+h)),
		}
	}},
	// E14: floor division
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(900) + 100
		b := rng.Intn(8) + 3
		return identityCase{
			Question: fmt.Sprintf("What is the integer part of %d divided by %d (floor division)?", a, b),
			Expected: fmt.Sprintf("%d", a/b),
		}
	}},
	// E15: sum of digits
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(9000) + 1000 // 4-digit
		sum := 0
		for tmp := n; tmp > 0; tmp /= 10 {
			sum += tmp % 10
		}
		return identityCase{
			Question: fmt.Sprintf("What is the sum of the digits of %d?", n),
			Expected: fmt.Sprintf("%d", sum),
		}
	}},
	// E16: GCD of two numbers
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(90) + 12
		b := rng.Intn(90) + 12
		return identityCase{
			Question: fmt.Sprintf("What is the greatest common divisor (GCD) of %d and %d?", a, b),
			Expected: fmt.Sprintf("%d", gcd(a, b)),
		}
	}},
	// E17: LCM of two numbers
	{Tier: "easy", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(20) + 4
		b := rng.Intn(20) + 4
		return identityCase{
			Question: fmt.Sprintf("What is the least common multiple (LCM) of %d and %d?", a, b),
			Expected: fmt.Sprintf("%d", a*b/gcd(a, b)),
		}
	}},
}

// --- MEDIUM templates (17) ---

var mediumTemplates = []identityTemplate{
	// M1: polynomial derivative  d/dx(ax^n) = a*n*x^(n-1)
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(8) + 2 // 2-9
		n := rng.Intn(4) + 3 // 3-6
		coeff := a * n
		exp := n - 1
		return identityCase{
			Question: fmt.Sprintf("What is the derivative of %dx^%d with respect to x? Give the coefficient and power in the form Cx^P.", a, n),
			Expected: fmt.Sprintf("%dx^%d", coeff, exp),
		}
	}},
	// M2: definite integral ∫₀ᵃ x^n dx = a^(n+1)/(n+1)
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(4) + 2 // 2-5
		n := rng.Intn(3) + 2 // 2-4
		num := 1
		for i := 0; i < n+1; i++ {
			num *= a
		}
		den := n + 1
		return identityCase{
			Question: fmt.Sprintf("Evaluate the definite integral of x^%d from 0 to %d. Give the exact value (fraction or integer).", n, a),
			Expected: formatFrac(num, den),
		}
	}},
	// M3: combinations C(n,k)
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(8) + 7  // 7-14
		k := rng.Intn(4) + 2  // 2-5
		return identityCase{
			Question: fmt.Sprintf("How many ways can you choose %d items from %d? (i.e., C(%d,%d))", k, n, n, k),
			Expected: fmt.Sprintf("%d", comb(n, k)),
		}
	}},
	// M4: modular arithmetic a^b mod m
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(8) + 2   // 2-9
		b := rng.Intn(8) + 5   // 5-12
		m := rng.Intn(20) + 7  // 7-26
		return identityCase{
			Question: fmt.Sprintf("What is %d^%d mod %d?", a, b, m),
			Expected: fmt.Sprintf("%d", modPow(a, b, m)),
		}
	}},
	// M5: geometric series sum  a*(r^n - 1)/(r - 1)
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(5) + 1  // 1-5
		r := rng.Intn(3) + 2  // 2-4
		n := rng.Intn(4) + 4  // 4-7
		rn := 1
		for i := 0; i < n; i++ {
			rn *= r
		}
		sum := a * (rn - 1) / (r - 1)
		return identityCase{
			Question: fmt.Sprintf("What is the sum of the first %d terms of a geometric series with first term %d and common ratio %d?", n, a, r),
			Expected: fmt.Sprintf("%d", sum),
		}
	}},
	// M6: sum of arithmetic series
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		a1 := rng.Intn(10) + 1  // 1-10
		d := rng.Intn(5) + 1    // 1-5
		n := rng.Intn(15) + 10  // 10-24
		sum := n * (2*a1 + (n-1)*d) / 2
		return identityCase{
			Question: fmt.Sprintf("What is the sum of the first %d terms of an arithmetic sequence starting at %d with common difference %d?", n, a1, d),
			Expected: fmt.Sprintf("%d", sum),
		}
	}},
	// M7: quadratic roots (integer roots guaranteed)
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		r1 := rng.Intn(17) - 8 // -8 to 8
		r2 := rng.Intn(17) - 8
		// x^2 - (r1+r2)x + r1*r2 = 0
		b := -(r1 + r2)
		c := r1 * r2
		bStr := ""
		if b > 0 {
			bStr = fmt.Sprintf(" + %dx", b)
		} else if b < 0 {
			bStr = fmt.Sprintf(" - %dx", -b)
		}
		cStr := ""
		if c > 0 {
			cStr = fmt.Sprintf(" + %d", c)
		} else if c < 0 {
			cStr = fmt.Sprintf(" - %d", -c)
		}
		lo, hi := r1, r2
		if lo > hi {
			lo, hi = hi, lo
		}
		expected := fmt.Sprintf("%d, %d || %d and %d", lo, hi, lo, hi)
		if lo == hi {
			expected = fmt.Sprintf("%d", lo)
		}
		return identityCase{
			Question: fmt.Sprintf("Find all real roots of x^2%s%s = 0. List them separated by comma, smallest first.", bStr, cStr),
			Expected: expected,
		}
	}},
	// M8: logarithm  log_b(b^n) = n
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		b := rng.Intn(7) + 2 // 2-8
		n := rng.Intn(5) + 2 // 2-6
		val := 1
		for i := 0; i < n; i++ {
			val *= b
		}
		return identityCase{
			Question: fmt.Sprintf("What is log base %d of %d?", b, val),
			Expected: fmt.Sprintf("%d", n),
		}
	}},
	// M9: permutations P(n,k) = n!/(n-k)!
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(6) + 5 // 5-10
		k := rng.Intn(3) + 2 // 2-4
		ans := factorial(n) / factorial(n-k)
		return identityCase{
			Question: fmt.Sprintf("How many permutations of %d items taken %d at a time? (i.e., P(%d,%d))", n, k, n, k),
			Expected: fmt.Sprintf("%d", ans),
		}
	}},
	// M10: binary to decimal
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(200) + 50 // 50-249
		return identityCase{
			Question: fmt.Sprintf("Convert the binary number %b to decimal.", n),
			Expected: fmt.Sprintf("%d", n),
		}
	}},
	// M11: system of 2 linear equations (integer solution guaranteed)
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		x := rng.Intn(11) - 5
		y := rng.Intn(11) - 5
		a1 := rng.Intn(5) + 1
		b1 := rng.Intn(5) + 1
		a2 := rng.Intn(5) + 1
		b2 := -(rng.Intn(5) + 1) // ensure non-parallel
		c1 := a1*x + b1*y
		c2 := a2*x + b2*y
		return identityCase{
			Question: fmt.Sprintf("Solve the system: %dx + %dy = %d and %dx + (%d)y = %d. Give x.", a1, b1, c1, a2, b2, c2),
			Expected: fmt.Sprintf("%d", x),
		}
	}},
	// M12: sum of squares 1^2+2^2+...+n^2 = n(n+1)(2n+1)/6
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(20) + 5 // 5-24
		ans := n * (n + 1) * (2*n + 1) / 6
		return identityCase{
			Question: fmt.Sprintf("What is the sum of squares from 1² to %d² (i.e., 1² + 2² + ... + %d²)?", n, n),
			Expected: fmt.Sprintf("%d", ans),
		}
	}},
	// M13: hexadecimal to decimal
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(3840) + 256 // 0x100 to 0xFFF
		return identityCase{
			Question: fmt.Sprintf("Convert the hexadecimal number %X to decimal.", n),
			Expected: fmt.Sprintf("%d", n),
		}
	}},
	// M14: Fibonacci(n)
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(15) + 10 // 10-24
		a, b := 0, 1
		for i := 2; i <= n; i++ {
			a, b = b, a+b
		}
		return identityCase{
			Question: fmt.Sprintf("What is the %dth Fibonacci number? (F(0)=0, F(1)=1, F(2)=1, ...)", n),
			Expected: fmt.Sprintf("%d", b),
		}
	}},
	// M15: probability (balls in urn)
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		red := rng.Intn(8) + 3
		blue := rng.Intn(8) + 3
		total := red + blue
		// P(both red) = C(red,2)/C(total,2)
		num := comb(red, 2)
		den := comb(total, 2)
		return identityCase{
			Question: fmt.Sprintf("An urn has %d red and %d blue balls. If you draw 2 without replacement, what is the probability both are red? Give as a fraction.", red, blue),
			Expected: formatFrac(num, den),
		}
	}},
	// M16: polynomial evaluation
	{Tier: "medium", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(5) + 1
		b := rng.Intn(10) - 5
		c := rng.Intn(10) - 5
		x := rng.Intn(7) + 2
		ans := a*x*x + b*x + c
		bStr := fmt.Sprintf("+ %d", b)
		if b < 0 {
			bStr = fmt.Sprintf("- %d", -b)
		}
		cStr := fmt.Sprintf("+ %d", c)
		if c < 0 {
			cStr = fmt.Sprintf("- %d", -c)
		}
		return identityCase{
			Question: fmt.Sprintf("Evaluate %dx² %sx %s at x = %d.", a, bStr, cStr, x),
			Expected: fmt.Sprintf("%d", ans),
		}
	}},
}

// --- HARD templates (17) ---

var hardTemplates = []identityTemplate{
	// H1: Euler's totient
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		candidates := []int{}
		for n := 30; n <= 200; n++ {
			if !isPrime(n) {
				candidates = append(candidates, n)
			}
		}
		n := candidates[rng.Intn(len(candidates))]
		return identityCase{
			Question: fmt.Sprintf("What is Euler's totient function φ(%d)?", n),
			Expected: fmt.Sprintf("%d", eulerTotient(n)),
		}
	}},
	// H2: 3x3 determinant
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		var m [3][3]int
		for i := 0; i < 3; i++ {
			for j := 0; j < 3; j++ {
				m[i][j] = rng.Intn(17) - 8 // -8 to 8
			}
		}
		return identityCase{
			Question: fmt.Sprintf("Compute the determinant of the 3x3 matrix [[%d,%d,%d],[%d,%d,%d],[%d,%d,%d]].",
				m[0][0], m[0][1], m[0][2], m[1][0], m[1][1], m[1][2], m[2][0], m[2][1], m[2][2]),
			Expected: fmt.Sprintf("%d", det3x3(m)),
		}
	}},
	// H3: Chinese Remainder Theorem  x ≡ a1 (mod m1), x ≡ a2 (mod m2)
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		// pick coprime moduli
		pairs := [][2]int{{3, 7}, {5, 7}, {7, 11}, {5, 11}, {7, 13}, {11, 13}, {3, 11}, {5, 13}}
		pair := pairs[rng.Intn(len(pairs))]
		m1, m2 := pair[0], pair[1]
		a1 := rng.Intn(m1)
		a2 := rng.Intn(m2)
		mod := m1 * m2
		// brute force CRT
		ans := -1
		for x := 0; x < mod; x++ {
			if x%m1 == a1 && x%m2 == a2 {
				ans = x
				break
			}
		}
		return identityCase{
			Question: fmt.Sprintf("Find the smallest non-negative integer x such that x ≡ %d (mod %d) and x ≡ %d (mod %d).", a1, m1, a2, m2),
			Expected: fmt.Sprintf("%d", ans),
		}
	}},
	// H4: sum of divisors σ(n)
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(150) + 50 // 50-199
		sum := 0
		for d := 1; d <= n; d++ {
			if n%d == 0 {
				sum += d
			}
		}
		return identityCase{
			Question: fmt.Sprintf("What is the sum of all positive divisors of %d (including 1 and %d itself)?", n, n),
			Expected: fmt.Sprintf("%d", sum),
		}
	}},
	// H5: double integral ∫₀ᵃ∫₀ᵇ (x+y) dy dx = ab(a+b)/2
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(5) + 2 // 2-6
		b := rng.Intn(5) + 2
		num := a * b * (a + b)
		return identityCase{
			Question: fmt.Sprintf("Evaluate the double integral ∫₀^%d ∫₀^%d (x+y) dy dx. Give the exact value (fraction or integer).", a, b),
			Expected: formatFrac(num, 2),
		}
	}},
	// H6: number of primes in range [a, b]
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		lo := rng.Intn(50) + 50   // 50-99
		hi := lo + rng.Intn(80) + 40 // +40 to +119
		count := 0
		for n := lo; n <= hi; n++ {
			if isPrime(n) {
				count++
			}
		}
		return identityCase{
			Question: fmt.Sprintf("How many prime numbers are there between %d and %d (inclusive)?", lo, hi),
			Expected: fmt.Sprintf("%d", count),
		}
	}},
	// H7: matrix trace of A^2 (2x2)
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		a, b := rng.Intn(11)-5, rng.Intn(11)-5
		c, d := rng.Intn(11)-5, rng.Intn(11)-5
		// A^2 = [[a*a+b*c, a*b+b*d],[c*a+d*c, c*b+d*d]]
		trace := (a*a + b*c) + (c*b + d*d)
		return identityCase{
			Question: fmt.Sprintf("Given the 2x2 matrix A = [[%d,%d],[%d,%d]], what is the trace of A^2 (i.e., tr(A²))?", a, b, c, d),
			Expected: fmt.Sprintf("%d", trace),
		}
	}},
	// H8: Beta function integral ∫₀¹ x^a (1-x)^b dx = a!b!/(a+b+1)!
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(3) + 1 // 1-3
		b := rng.Intn(3) + 1
		num := factorial(a) * factorial(b)
		den := factorial(a + b + 1)
		return identityCase{
			Question: fmt.Sprintf("Evaluate ∫₀¹ x^%d · (1-x)^%d dx. Give the exact value as a fraction.", a, b),
			Expected: formatFrac(num, den),
		}
	}},
	// H9: number of divisors
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(400) + 100 // 100-499
		count := 0
		for d := 1; d <= n; d++ {
			if n%d == 0 {
				count++
			}
		}
		return identityCase{
			Question: fmt.Sprintf("How many positive divisors does %d have?", n),
			Expected: fmt.Sprintf("%d", count),
		}
	}},
	// H10: modular inverse  a^(-1) mod m (a,m coprime)
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		primes := []int{7, 11, 13, 17, 19, 23, 29, 31}
		m := primes[rng.Intn(len(primes))]
		a := rng.Intn(m-2) + 2 // 2..m-1, coprime since m is prime
		// a^(-1) mod m = a^(m-2) mod m (Fermat)
		inv := modPow(a, m-2, m)
		return identityCase{
			Question: fmt.Sprintf("What is the modular multiplicative inverse of %d modulo %d?", a, m),
			Expected: fmt.Sprintf("%d", inv),
		}
	}},
	// H11: Stirling number of second kind S(n,2) = 2^(n-1) - 1
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(8) + 4 // 4-11
		ans := (1 << (n - 1)) - 1
		return identityCase{
			Question: fmt.Sprintf("What is the Stirling number of the second kind S(%d, 2)?", n),
			Expected: fmt.Sprintf("%d", ans),
		}
	}},
	// H12: Catalan number C_n = C(2n,n)/(n+1)
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(7) + 3 // 3-9
		ans := comb(2*n, n) / (n + 1)
		return identityCase{
			Question: fmt.Sprintf("What is the %dth Catalan number? (C_0=1, C_1=1, C_2=2, C_3=5, ...)", n),
			Expected: fmt.Sprintf("%d", ans),
		}
	}},
	// H13: eigenvalue sum (trace) and product (det) of 2x2
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(11) - 5
		b := rng.Intn(11) - 5
		c := rng.Intn(11) - 5
		d := rng.Intn(11) - 5
		det := a*d - b*c
		return identityCase{
			Question: fmt.Sprintf("What is the product of the eigenvalues of the matrix [[%d,%d],[%d,%d]]?", a, b, c, d),
			Expected: fmt.Sprintf("%d", det),
		}
	}},
	// H14: triple integral ∫₀ᵃ∫₀ᵇ∫₀ᶜ xyz dz dy dx = (abc)²/8
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		a := rng.Intn(3) + 2
		b := rng.Intn(3) + 2
		c := rng.Intn(3) + 2
		num := a * a * b * b * c * c
		den := 8
		return identityCase{
			Question: fmt.Sprintf("Evaluate the triple integral ∫₀^%d ∫₀^%d ∫₀^%d xyz dz dy dx. Give exact value (fraction or integer).", a, b, c),
			Expected: formatFrac(num, den),
		}
	}},
	// H15: derangements D(n) = n! * Σ(-1)^k/k! for k=0..n
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		n := rng.Intn(5) + 4 // 4-8
		nf := factorial(n)
		// D(n) = n! * (1 - 1/1! + 1/2! - 1/3! + ... + (-1)^n/n!)
		// = Σ (-1)^k * n!/k! for k=0..n
		ans := 0
		for k := 0; k <= n; k++ {
			term := nf / factorial(k)
			if k%2 == 0 {
				ans += term
			} else {
				ans -= term
			}
		}
		return identityCase{
			Question: fmt.Sprintf("How many derangements (permutations with no fixed points) are there of %d elements?", n),
			Expected: fmt.Sprintf("%d", ans),
		}
	}},
	// H16: sum of geometric series (fractional ratio) a*(1-r^n)/(1-r) with r=1/k
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		// S = Σ_{i=0}^{n-1} k^i = (k^n - 1)/(k-1)
		// Ask: sum of 1 + 1/k + 1/k^2 + ... + 1/k^(n-1) = S / k^(n-1)
		// Simpler: just ask sum of k^0 + k^1 + ... + k^(n-1)
		k := rng.Intn(3) + 2  // 2-4
		n := rng.Intn(5) + 5  // 5-9
		kn := 1
		for i := 0; i < n; i++ {
			kn *= k
		}
		ans := (kn - 1) / (k - 1)
		return identityCase{
			Question: fmt.Sprintf("What is %d^0 + %d^1 + %d^2 + ... + %d^%d?", k, k, k, k, n-1),
			Expected: fmt.Sprintf("%d", ans),
		}
	}},
	// H17: Wilson's theorem check — (p-1)! mod p = p-1 for prime p
	{Tier: "hard", Gen: func(rng *rand.Rand) identityCase {
		primes := []int{11, 13, 17, 19, 23}
		p := primes[rng.Intn(len(primes))]
		// (p-1)! mod p
		f := 1
		for i := 2; i < p; i++ {
			f = (f * i) % p
		}
		return identityCase{
			Question: fmt.Sprintf("What is %d! mod %d? (i.e., factorial of %d, modulo %d)", p-1, p, p-1, p),
			Expected: fmt.Sprintf("%d", f),
		}
	}},
}

var identityTemplates []identityTemplate

func init() {
	identityTemplates = append(identityTemplates, easyTemplates...)
	identityTemplates = append(identityTemplates, mediumTemplates...)
	identityTemplates = append(identityTemplates, hardTemplates...)
}

// roundFloat rounds to n decimal places.
func roundFloat(v float64, n int) float64 {
	p := math.Pow(10, float64(n))
	return math.Round(v*p) / p
}
