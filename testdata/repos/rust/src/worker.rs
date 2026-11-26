use crate::helper;
pub struct User;
impl User {
    pub fn new() { helper(); Self::save(); }
    pub fn save(&self) { self.validate(); }
    fn validate(&self) { helper(); }
}
impl crate::Local for User {
    fn save(&self) { self.validate(); }
}
pub struct List<T> { items: Vec<T> }
impl<T> List<T> {
    pub fn len(&self) { Self::empty(); }
    fn empty() {}
}
pub fn work() { helper(); User::new(); }
